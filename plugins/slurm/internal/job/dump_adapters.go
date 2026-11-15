package job

import (
	"context"
	"fmt"
	"os"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	criu_client "github.com/cedana/cedana/pkg/criu"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/pkg/utils"
	slurm_keys "github.com/cedana/cedana/plugins/slurm/pkg/keys"
	slurm_utils "github.com/cedana/cedana/plugins/slurm/pkg/utils"
	"github.com/opencontainers/cgroups"
	cgroupsManager "github.com/opencontainers/cgroups/manager"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func GetSlurmJobForDump(next types.Dump) types.Dump {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (code func() <-chan int, err error) {
		jid := req.GetDetails().GetSlurm().GetJobID()
		pid := req.GetDetails().GetSlurm().GetPID()

		path, err := ResolveJobCgroupPath(jid, pid)
		if err != nil {
			return nil, err
		}

		config := &cgroups.Cgroup{
			Path:      path,
			Resources: &cgroups.Resources{},
		}
		manager, err := cgroupsManager.New(config)
		if err != nil {
			return nil, status.Errorf(codes.NotFound, "failed to load cgroup2 for slurm job %d: %v", jid, err)
		}

		ctx = context.WithValue(ctx, slurm_keys.CGROUP_MANAGER_CONTEXT_KEY, manager)

		return next(ctx, opts, resp, req)
	}
}

func SetPIDForDump(next types.Dump) types.Dump {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (code func() <-chan int, err error) {
		if resp.State == nil {
			resp.State = &daemon.ProcessState{}
		}

		if resp.GetState().GetPID() == 0 {
			pid := req.GetDetails().GetSlurm().GetPID()
			if pid == 0 {
				return nil, status.Errorf(codes.NotFound, "failed to get PID from slurm details")
			}
			resp.State.PID = pid
		}

		state := resp.GetState()

		err = utils.FillProcessState(ctx, state.PID, state, true)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to fill process state: %v", err)
		}

		return next(ctx, opts, resp, req)
	}
}

// We manually restore the SLURM script because SLURM
// will delete the script used to launch the job step
func DumpSlurmScript(next types.Dump) types.Dump {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (code func() <-chan int, err error) {
		state := resp.GetState()
		if state == nil {
			log.Warn().Msg("no process info found. it should have been set by an adapter")
			return next(ctx, opts, resp, req)
		}

		// Slurm's spooled script, which the job's shell runs from
		for _, f := range state.GetOpenFiles() {
			if path := f.GetPath(); isSpoolScript(path) {
				if err := saveSpoolScript(opts, state.PID, f.GetFd(), path); err != nil {
					return nil, status.Errorf(codes.Internal, "failed to save the slurm script: %v", err)
				}
				break
			}
		}

		return next(ctx, opts, resp, req)
	}
}

// saveSpoolScript saves the script that the shell at pid holds open on fd, read through that
// descriptor, and records the shell's offset in it once the job is frozen. Callbacks run in reverse
// order of registration, so the record is written before any compression or upload of the dump.
// A failure to record it is only logged: a restore then reopens the spool file as before, which
// works for the same job.
func saveSpoolScript(opts types.Opts, pid uint32, fd uint64, path string) error {
	if opts.DumpFs == nil {
		return fmt.Errorf("no dump filesystem")
	}
	link := fmt.Sprintf("/proc/%d/fd/%d", pid, fd)
	if target, err := os.Readlink(link); err != nil || target != path {
		return fmt.Errorf("%s is no longer open on descriptor %d of %d", path, fd, pid)
	}
	script, err := os.Open(link)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	defer script.Close()
	if err := slurm_utils.SaveScriptToDump(script, SPOOL_SCRIPT_FILE, opts.DumpFs); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}

	dumpFs := opts.DumpFs
	opts.CRIUCallback.Include(&criu_client.NotifyCallback{
		PostDumpFunc: func(ctx context.Context, _ *criu_proto.CriuOpts) error {
			offset, err := fdOffset(pid, fd)
			if err == nil {
				err = saveSpoolScriptRecord(dumpFs, spoolScriptRecord{Path: path, Offset: offset})
			}
			if err != nil {
				log.Warn().Err(err).Str("path", path).Msg("failed to record the slurm script's offset: only the same job can restore this dump")
			}
			return nil
		},
	})
	return nil
}

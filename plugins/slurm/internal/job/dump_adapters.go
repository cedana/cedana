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
	"github.com/spf13/afero"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	// What cedana-slurm's wrapper names the script it runs the job with, cedana-script-<jobid>.sh.
	// The shell keeps it open for as long as the job runs, and it is gone by the time the job
	// is restored.
	SLURM_SCRIPT_PREFIX = "cedana-script"
	// What it is kept as in the dump, and where its mode and owner go. CRIU wants the file
	// back exactly as it was.
	SLURM_SCRIPT_FILE       = "cedana-script"
	SLURM_SCRIPT_ATTRS_FILE = "cedana-script.json"
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

		// The shell holds the script open, so a restore without it, or with it not as it was,
		// fails. Better to fail the dump.
		var saveErr error
		utils.WalkTree(state, "OpenFiles", "Children", func(f *daemon.File) bool {
			if path := f.GetPath(); isJobScript(path) {
				saveErr = saveJobScript(opts.DumpFs, state.PID, path)
				return false
			}
			return true
		})
		if saveErr != nil {
			return nil, status.Errorf(codes.Internal, "failed to save the slurm script: %v", saveErr)
		}

		// Slurm's spooled script, which the shell runs from when its script isn't rewritten
		for _, f := range state.GetOpenFiles() {
			if path := f.GetPath(); isSpoolScript(path) {
				if err := saveSpoolScript(opts, state.PID, f.GetFd(), path); err != nil {
					return nil, status.Errorf(codes.Internal, "failed to save the slurm script: %v", err)
				}
				warnIfScriptReread(state, path)
				break
			}
		}

		return next(ctx, opts, resp, req)
	}
}

// saveJobScript keeps the script at path, open in the job of pid, in the dump with its
// attributes. The job may have it where we don't, e.g. in a private /tmp.
func saveJobScript(dumpFs afero.Fs, pid uint32, path string) error {
	if dumpFs == nil {
		return fmt.Errorf("no dump filesystem")
	}
	script, err := os.Open(inRootOf(pid, path))
	if err != nil {
		return err
	}
	defer script.Close()

	if err := slurm_utils.SaveScriptToDump(script, SLURM_SCRIPT_FILE, dumpFs); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	info, err := script.Stat()
	if err != nil {
		return err
	}
	if err := saveScriptAttrs(dumpFs, info); err != nil {
		return fmt.Errorf("attributes of %s: %w", path, err)
	}
	return nil
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

// warnIfScriptReread logs when processes in the job read the spooled script at different offsets,
// e.g. a script running bash "$0" or source "$0": after a restore all of them read from the
// shell's position. Forked subshells share the shell's open, and its offset, so they don't count.
func warnIfScriptReread(state *daemon.ProcessState, path string) {
	offsets := map[int64]bool{}
	var walk func(s *daemon.ProcessState)
	walk = func(s *daemon.ProcessState) {
		for _, f := range s.GetOpenFiles() {
			if f.GetPath() == path {
				if offset, err := fdOffset(s.GetPID(), f.GetFd()); err == nil {
					offsets[offset] = true
				}
			}
		}
		for _, child := range s.GetChildren() {
			walk(child)
		}
	}
	walk(state)
	if len(offsets) > 1 {
		log.Warn().Str("path", path).Msg("the job reads its script at several offsets (e.g. bash \"$0\"): after a restore, all of them read from the shell's position")
	}
}

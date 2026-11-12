package job

import (
	"context"
	"fmt"
	"os"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
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

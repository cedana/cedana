package job

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
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

const SLURM_SCRIPT_FILE = "slurm_script"

func GetSlurmJobForDump(next types.Dump) types.Dump {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (code func() <-chan int, err error) {
		jid := req.GetDetails().GetSlurm().GetJobID()
		hostname := req.GetDetails().GetSlurm().GetHostname()

		path := fmt.Sprintf("/system.slice/%s_slurmstepd.scope/job_%d/step_batch/user/task_special", hostname, jid)
		if _, err := os.Stat("/sys/fs/cgroup" + path); os.IsNotExist(err) {
			return nil, status.Errorf(codes.NotFound, "cgroup path for slurm job %d does not exist: %s", jid, path)
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

		utils.WalkTree(state, "OpenFiles", "Children", func(f *daemon.File) bool {
			if path := f.GetPath(); filepath.Base(path) == "slurm_script" {
				script, err := os.Open(path)
				if err != nil {
					log.Warn().Err(err).Msgf("failed to open slurm script file %s", path)
					return false
				}
				defer script.Close()

				slurm_utils.SaveScriptToDump(script, SLURM_SCRIPT_FILE, opts.DumpFs)
				return false
			}
			return true
		})

		return next(ctx, opts, resp, req)
	}
}

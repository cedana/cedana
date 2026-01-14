package job

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"

	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/rs/zerolog/log"

	slurm_keys "github.com/cedana/cedana/plugins/slurm/pkg/keys"
	slurm_utils "github.com/cedana/cedana/plugins/slurm/pkg/utils"

	"github.com/gogo/protobuf/proto"
	"github.com/opencontainers/cgroups"
	cgroupsManager "github.com/opencontainers/cgroups/manager"
	"github.com/opencontainers/runc/libcontainer/configs"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func GetSlurmJobForRestore(next types.Restore) types.Restore {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (code func() <-chan int, err error) {
		details := req.GetDetails().GetSlurm()
		jid := details.GetJobID()
		hostname := details.GetHostname()
		parent := details.GetParentPID()

		path := fmt.Sprintf("/system.slice/%s_slurmstepd.scope/job_%d/step_batch/user/task_special", hostname, jid)
		if _, err := os.Stat("/sys/fs/cgroup" + path); os.IsNotExist(err) {
			return nil, status.Errorf(codes.NotFound, "cgroup path for slurm job %d does not exist: %s", jid, path)
		}

		// Set the new job PID to be the PID of the restored process
		if req.Criu == nil {
			req.Criu = &criu_proto.CriuOpts{}
		}
		req.Criu.Pid = proto.Int32(int32(parent))
		req.Criu.ShellJob = proto.Bool(true)

		// Get the cgroup of the restored job slurmstepd
		config := &cgroups.Cgroup{
			Path:      path,
			Resources: &cgroups.Resources{},
		}
		manager, err := cgroupsManager.New(config)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to load cgroup2 for slurm job %d: %v", jid, err)
		}
		ctx = context.WithValue(ctx, slurm_keys.CGROUP_MANAGER_CONTEXT_KEY, manager)

		// Check that cgroup is not frozen. Do not use Exists() here
		// since in cgroup v1 it only checks "devices" controller.
		st, err := manager.GetFreezerState()
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to get cgroup freezer state: %v", err)
		}
		if st == configs.Frozen {
			return nil, status.Errorf(codes.FailedPrecondition, "container's cgroup unexpectedly frozen")
		}

		return next(ctx, opts, resp, req)
	}
}

// We manually restore the SLURM script because SLURM
// will delete the script used to launch the job step
func RestoreSlurmScript(next types.Restore) types.Restore {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (code func() <-chan int, err error) {
		state := resp.GetState()
		if state == nil {
			log.Warn().Msg("no process info found. it should have been filled by an adapter")
			return next(ctx, opts, resp, req)
		}

		utils.WalkTree(state, "OpenFiles", "Children", func(f *daemon.File) bool {
			if path := f.GetPath(); filepath.Base(path) == SLURM_SCRIPT_FILE {
				contents, err := slurm_utils.LoadScriptFromDump(path, opts.DumpFs)
				if err != nil {
					log.Warn().Err(err).Msgf("failed to load slurm script from dump %s", path)
					return false
				}

				err = os.MkdirAll(filepath.Dir(path), 0755)
				if err != nil {
					log.Warn().Err(err).Msgf("failed to create directory for slurm script %s", path)
					return false
				}

				err = os.WriteFile(path, contents, 0755)
				if err != nil {
					log.Warn().Err(err).Msgf("failed to restore slurm script file %s", path)
					return false
				}

				return false
			}

			return true
		})

		return next(ctx, opts, resp, req)
	}
}

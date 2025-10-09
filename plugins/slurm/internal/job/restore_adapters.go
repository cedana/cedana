package job

import (
	"context"
	"fmt"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/pkg/types"
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
		path := fmt.Sprintf("/system.slice/%s_slurmstepd.scope/job_%d/step_batch_user/task_special", hostname, jid)

		// Set the new job PID to be the PID of the restored process
		if req.Criu == nil {
			req.Criu = &criu_proto.CriuOpts{}
		}
		req.Criu.Pid = parent

		// Get the cgroup of the restored job slurmstepd
		config := &cgroups.Cgroup{
			Path:      path,
			Resources: &cgroups.Resources{},
		}
		manager, err := cgroupsManager.New(config)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to load cgroup2 for slurm job %d: %v", jid, err)
		}

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

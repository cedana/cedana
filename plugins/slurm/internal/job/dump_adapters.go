package job

import (
	"context"
	"fmt"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/types"
	slurm_keys "github.com/cedana/cedana/plugins/slurm/pkg/keys"
	"github.com/opencontainers/cgroups"
	cgroupsManager "github.com/opencontainers/cgroups/manager"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func GetSlurmJobForDump(next types.Dump) types.Dump {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (code func() <-chan int, err error) {
		jid := req.GetDetails().GetSlurm().GetJobID()
		hostname := req.GetDetails().GetSlurm().GetHostname()
		path := fmt.Sprintf("/system.slice/%s_slurmstepd.scope/job_%d/step_batch_user/task_special", hostname, jid)

		log.Trace().Str("path", path).Int32("jid", jid).Msg("loading cgroup2 for slurm job")

		config := &cgroups.Cgroup{
			Path:      path,
			Resources: &cgroups.Resources{},
		}
		manager, err := cgroupsManager.New(config)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to load cgroup2 for slurm job %d: %v", jid, err)
		}

		ctx = context.WithValue(ctx, slurm_keys.CGROUP_MANAGER_CONTEXT_KEY, manager)

		return nil, nil
	}
}

func SetPIDForDump(next types.Dump) types.Dump {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (code func() <-chan int, err error) {
		if resp.State == nil {
			resp.State = &daemon.ProcessState{}
		}
		resp.State.PID = int32(req.GetDetails().GetSlurm().GetPID())

		return next(ctx, opts, resp, req)
	}
}

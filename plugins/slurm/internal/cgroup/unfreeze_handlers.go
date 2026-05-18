package cgroup

import (
	"context"
	"fmt"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/types"
	slurm_keys "github.com/cedana/cedana/plugins/slurm/pkg/keys"
	"github.com/opencontainers/cgroups"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var Unfreeze types.Unfreeze = unfreeze

func unfreeze(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (code func() <-chan int, err error) {
	manager, ok := ctx.Value(slurm_keys.CGROUP_MANAGER_CONTEXT_KEY).(cgroups.Manager)
	if !ok {
		return nil, status.Errorf(codes.FailedPrecondition, "failed to get cgroup manager from context")
	}

	freezerState, err := manager.GetFreezerState()
	if err != nil {
		return nil, status.Error(codes.Internal, fmt.Sprintf("failed to get cgroup freezer state: %v", err))
	}

	if freezerState != cgroups.Frozen {
		return nil, fmt.Errorf("container cgroup is not frozen")
	}

	return nil, manager.Freeze(cgroups.Thawed)
}

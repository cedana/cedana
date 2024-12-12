package container

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/types"
	runc_keys "github.com/cedana/cedana/plugins/runc/pkg/keys"
	"github.com/opencontainers/runc/libcontainer"
	"github.com/opencontainers/runc/libcontainer/cgroups/manager"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func GetContainerForDump(next types.Dump) types.Dump {
	return func(ctx context.Context, server types.ServerOpts, resp *daemon.DumpResp, req *daemon.DumpReq) (chan int, error) {
		root := req.GetDetails().GetRunc().GetRoot()
		id := req.GetDetails().GetRunc().GetID()

		container, err := libcontainer.Load(root, id)
		if err != nil {
			return nil, status.Errorf(codes.NotFound, "failed to load container: %v", err)
		}

		state, err := container.State()
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to get container state: %v", err)
		}

		// XXX: Create new cgroup manager, as the container's cgroup manager is not accessible (internal)
		manager, err := manager.NewWithPaths(state.Config.Cgroups, state.CgroupPaths)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to create cgroup manager: %v", err)
		}

		ctx = context.WithValue(ctx, runc_keys.CONTAINER_CGROUP_MANAGER_CONTEXT_KEY, manager)
		ctx = context.WithValue(ctx, runc_keys.CONTAINER_CONTEXT_KEY, container)

		return next(ctx, server, resp, req)
	}
}

func SetPIDForDump(next types.Dump) types.Dump {
	return func(ctx context.Context, server types.ServerOpts, resp *daemon.DumpResp, req *daemon.DumpReq) (chan int, error) {
		container, ok := ctx.Value(runc_keys.CONTAINER_CONTEXT_KEY).(*libcontainer.Container)
		if !ok {
			return nil, status.Errorf(codes.FailedPrecondition, "failed to get container from context")
		}

		state, err := container.State()
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to get container state: %v", err)
		}

		if resp.State == nil {
			resp.State = &daemon.ProcessState{}
		}
		resp.State.PID = uint32(state.InitProcessPid)

		return next(ctx, server, resp, req)
	}
}

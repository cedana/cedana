package container

import (
	"context"
	"fmt"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/types"
	runc_keys "github.com/cedana/cedana/plugins/runc/pkg/keys"
	"github.com/opencontainers/cgroups"
	"github.com/opencontainers/cgroups/manager"
	"github.com/opencontainers/runc/libcontainer"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func GetContainerForDump(next types.Dump) types.Dump {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (code func() <-chan int, err error) {
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

		var cgroupManager cgroups.Manager

		// Low level, read cgroup paths from proc
		if state.InitProcessPid > 0 {
			cgroupFile := fmt.Sprintf("/proc/%d/cgroup", state.InitProcessPid)
			currentPaths, err := cgroups.ParseCgroupFile(cgroupFile)
			if err == nil {
				log.Debug().Interface("currentPaths", currentPaths).Msg("using current cgroup paths for dump")
				cgroupManager, err = manager.NewWithPaths(state.Config.Cgroups, currentPaths)
				if err != nil {
					log.Debug().Err(err).Msg("failed to create cgroup manager with current paths, falling back to container state paths")
				}
			} else {
				log.Debug().Err(err).Msg("failed to read current cgroup paths, falling back to cached paths")
			}
		}

		// BS:
		// Fall back to getting cgroup paths from the container state
		// This is broken though for crcr but will leave it just in case for single cr
		if cgroupManager == nil {
			log.Debug().Interface("cachedPaths", state.CgroupPaths).Msg("using cached cgroup paths for dump")
			cgroupManager, err = manager.NewWithPaths(state.Config.Cgroups, state.CgroupPaths)
			if err != nil {
				return nil, status.Errorf(codes.Internal, "failed to create cgroup manager: %v", err)
			}
		}

		ctx = context.WithValue(ctx, runc_keys.CONTAINER_CGROUP_MANAGER_CONTEXT_KEY, cgroupManager)
		ctx = context.WithValue(ctx, runc_keys.CONTAINER_CONTEXT_KEY, container)

		return next(ctx, opts, resp, req)
	}
}

func SetPIDForDump(next types.Dump) types.Dump {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (code func() <-chan int, err error) {
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

		return next(ctx, opts, resp, req)
	}
}

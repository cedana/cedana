package cgroup

import (
	"context"
	"fmt"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/pkg/criu"
	"github.com/cedana/cedana/pkg/types"
	runc_keys "github.com/cedana/cedana/plugins/runc/pkg/keys"
	"github.com/opencontainers/runc/libcontainer"
	"github.com/opencontainers/runc/libcontainer/cgroups"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	// WARN: DO NOT REMOVE THIS IMPORT. Has side effects.
	// See -> 'github.com/opencontainers/runc/libcontainer/cgroups/cgroups.go'
	_ "github.com/opencontainers/runc/libcontainer/cgroups/devices"
)

// Sets the ManageCgroups field in the criu options to true.
func ManageCgroupsForRestore(mode criu_proto.CriuCgMode) types.Adapter[types.Restore] {
	return func(next types.Restore) types.Restore {
		return func(ctx context.Context, server types.ServerOpts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (chan int, error) {
			if req.GetCriu() == nil {
				req.Criu = &criu_proto.CriuOpts{}
			}

			req.Criu.ManageCgroups = proto.Bool(true)
			req.Criu.ManageCgroupsMode = &mode

			return next(ctx, server, resp, req)
		}
	}
}

// Adds a initialize hook that applies cgroups to the CRIU process as soon as it is started.
func ApplyCgroupsOnRestore(next types.Restore) types.Restore {
	return func(ctx context.Context, server types.ServerOpts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (chan int, error) {
		container, ok := ctx.Value(runc_keys.CONTAINER_CONTEXT_KEY).(*libcontainer.Container)
		if !ok {
			return nil, status.Errorf(codes.FailedPrecondition, "failed to get container from context")
		}
		manager, ok := ctx.Value(runc_keys.CONTAINER_CGROUP_MANAGER_CONTEXT_KEY).(cgroups.Manager)
		if !ok {
			return nil, status.Errorf(codes.FailedPrecondition, "failed to get cgroup manager from context")
		}

		if req.Criu == nil {
			req.Criu = &criu_proto.CriuOpts{}
		}

		config := container.Config()

		callback := &criu.NotifyCallback{
			InitializeFunc: func(ctx context.Context, criuPid int32) error {
				err := manager.Apply(int(criuPid))
				if err != nil {
					return fmt.Errorf("failed to apply cgroups: %v", err)
				}
				err = manager.Set(config.Cgroups.Resources)
				if err != nil {
					return fmt.Errorf("failed to set cgroup resources: %v", err)
				}

				// TODO Should we use c.cgroupManager.GetPaths()
				// instead of reading /proc/pid/cgroup?
				path := fmt.Sprintf("/proc/%d/cgroup", criuPid)
				cgroupsPaths, err := cgroups.ParseCgroupFile(path)
				if err != nil {
					return err
				}
				for c, p := range cgroupsPaths {
					cgroupRoot := &criu_proto.CgroupRoot{
						Ctrl: proto.String(c),
						Path: proto.String(p),
					}
					req.Criu.CgRoot = append(req.Criu.CgRoot, cgroupRoot)
				}

				return nil
			},
		}
		server.CRIUCallback.Include(callback)

		return next(ctx, server, resp, req)
	}
}

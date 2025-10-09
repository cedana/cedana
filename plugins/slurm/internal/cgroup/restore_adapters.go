package cgroup

import (
	"context"
	"fmt"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/pkg/criu"
	"github.com/cedana/cedana/pkg/types"
	slurm_keys "github.com/cedana/cedana/plugins/slurm/pkg/keys"
	"github.com/opencontainers/cgroups"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	// WARN: DO NOT REMOVE THIS IMPORT. Has side effects.
	// See -> 'github.com/opencontainers/runc/libcontainer/cgroups/cgroups.go'
	_ "github.com/opencontainers/cgroups/devices"
)

// Adds a initialize hook that applies cgroups to the CRIU process as soon as it is started.
func ApplyCgroupsOnRestore(next types.Restore) types.Restore {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (code func() <-chan int, err error) {
		manager, ok := ctx.Value(slurm_keys.CGROUP_MANAGER_CONTEXT_KEY).(cgroups.Manager)
		if !ok {
			return nil, status.Errorf(codes.FailedPrecondition, "failed to get cgroup manager from context")
		}

		if req.Criu == nil {
			req.Criu = &criu_proto.CriuOpts{}
		}

		callback := &criu.NotifyCallback{
			InitializeFunc: func(ctx context.Context, criuPid int32) (err error) {
				var paths map[string]string

				err = manager.Apply(int(criuPid))
				if err != nil {
					return fmt.Errorf("failed to apply cgroups: %v", err)
				}
				// err = manager.Set(config.Cgroups.Resources)  // TODO: may need to set resources if cedana-slurm doesn't do it
				// if err != nil {
				// 	return fmt.Errorf("failed to set cgroup resources: %v", err)
				// }
				paths = manager.GetPaths()

				for c, p := range paths {
					cgroupRoot := &criu_proto.CgroupRoot{
						Ctrl: proto.String(c),
						Path: proto.String(p),
					}
					req.Criu.CgRoot = append(req.Criu.CgRoot, cgroupRoot)
				}

				return nil
			},
		}
		opts.CRIUCallback.Include(callback)

		return next(ctx, opts, resp, req)
	}
}

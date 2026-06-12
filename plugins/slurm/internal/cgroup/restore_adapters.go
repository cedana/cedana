//go:build linux

package cgroup

import (
	"context"
	"fmt"
	"strings"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/pkg/criu"
	"github.com/cedana/cedana/pkg/types"
	slurm_keys "github.com/cedana/cedana/plugins/slurm/pkg/keys"
	"github.com/opencontainers/cgroups"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	// WARN: DO NOT REMOVE THIS IMPORT. Has side effects.
	// See -> 'github.com/opencontainers/runc/libcontainer/cgroups/cgroups.go'
	_ "github.com/opencontainers/cgroups/devices"
)

const CGROUPS_BASE_PATH = "/sys/fs/cgroup"

func ApplyCgroupsOnRestore(next types.Restore) types.Restore {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (code func() <-chan int, err error) {
		manager, ok := ctx.Value(slurm_keys.CGROUP_MANAGER_CONTEXT_KEY).(cgroups.Manager)
		if !ok {
			return nil, status.Errorf(codes.FailedPrecondition, "failed to get cgroup manager from context")
		}

		if req.Criu == nil {
			return nil, status.Errorf(codes.InvalidArgument, "missing CRIU options in restore request")
		}

		// Disable CRIU cgroup management: CRIU cannot remap cross-job cgroup paths
		// (e.g. job_1219/step_batch/task_0 -> job_1266/step_batch). With manage_cgroups
		// disabled, restored processes inherit CRIU's cgroup, which we place into the
		// new job's hierarchy via manager.Apply below.
		req.Criu.ManageCgroupsMode = criu_proto.CriuCgMode_CG_NONE.Enum()
		req.Criu.ManageCgroups = proto.Bool(true)

		callback := &criu.NotifyCallback{
			InitializeFunc: func(ctx context.Context, criuPid int32) (err error) {
				err = manager.Apply(int(criuPid))
				if err != nil {
					return fmt.Errorf("failed to apply cgroups: %v", err)
				}
				paths := manager.GetPaths()

				for c, p := range paths {
					p = strings.TrimPrefix(p, CGROUPS_BASE_PATH)
					log.Debug().Str("controller", c).Str("path", p).Msg("setting cgroup root for CRIU")
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

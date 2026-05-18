//go:build linux

package cgroup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"syscall"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
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
		req.Criu.ManageCgroups = proto.Bool(false)

		callback := &criu.NotifyCallback{
			InitializeFunc: func(ctx context.Context, criuPid int32) (err error) {
				paths := manager.GetPaths()

				log.Trace().Msgf("restoring checkpoint from old cgroup to new cgroup\n")
				log.Trace().Msgf("CRIU process PID: %d\n", criuPid)
				log.Trace().Msgf("new cgroup paths:\n")
				for c, p := range paths {
					log.Trace().Msgf("  Controller %s: %s\n", c, p)
				}

				// log whether the new cgroup hierarchy exists
				for _, p := range paths {
					if err := os.MkdirAll(p, 0755); os.IsNotExist(err) {
						log.Trace().Msgf("cgroup path does not exist: %s\n", p)
					} else {
						log.Trace().Msgf("cgroup path already exists: %s\n", p)
					}
				}

				// apply cgroups to the CRIU process
				err = manager.Apply(int(criuPid))
				if err != nil {
					if os.IsPermission(err) || errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EPERM) {
						log.Warn().Msgf("skipping cgroup apply (unprivileged): %v\n", err)
					} else {
						return fmt.Errorf("failed to apply cgroups to CRIU process: %v", err)
					}
				} else {
					log.Trace().Msgf("applied cgroups to CRIU process %d\n", criuPid)
				}

				return nil
			},
		}
		opts.CRIUCallback.Include(callback)

		return next(ctx, opts, resp, req)
	}
}

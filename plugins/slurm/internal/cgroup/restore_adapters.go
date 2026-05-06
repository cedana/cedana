//go:build linux

package cgroup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"syscall"

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

func ApplyCgroupsOnRestore(next types.Restore) types.Restore {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (code func() <-chan int, err error) {
		manager, ok := ctx.Value(slurm_keys.CGROUP_MANAGER_CONTEXT_KEY).(cgroups.Manager)
		if !ok {
			return nil, status.Errorf(codes.FailedPrecondition, "failed to get cgroup manager from context")
		}

		if req.Criu == nil {
			return nil, status.Errorf(codes.InvalidArgument, "missing CRIU options in restore request")
		}

		// GetPaths() returns absolute filesystem paths (e.g., /sys/fs/cgroup/cpu/slurm/...).
		// GetCgroups().Path is the relative cgroup path within each controller hierarchy,
		// which is what CRIU expects for cg_root.
		cgroupConfig, err := manager.GetCgroups()
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to get cgroup config: %v", err)
		}
		relativePath := cgroupConfig.Path

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

				// set CgRoot to tell CRIU where to place restored processes
				// CRIU expects paths relative to each controller's mount point
				for c := range paths {
					cgroupRoot := &criu_proto.CgroupRoot{
						Ctrl: proto.String(c),
						Path: proto.String(relativePath),
					}
					req.Criu.CgRoot = append(req.Criu.CgRoot, cgroupRoot)
					log.Trace().Msgf("set CgRoot for controller %s: %s\n", c, relativePath)
				}

				return nil
			},
		}
		opts.CRIUCallback.Include(callback)

		return next(ctx, opts, resp, req)
	}
}

package cgroup

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

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
			req.Criu = &criu_proto.CriuOpts{}
		}

		if req.Criu.ManageCgroupsMode != nil || true { // Force this path
			manageCgroups := criu_proto.CriuCgMode_IGNORE
			req.Criu.ManageCgroupsMode = &manageCgroups
		} else {
			manageCgroups := false
			req.Criu.ManageCgroups = &manageCgroups
		}

		callback := &criu.NotifyCallback{
			InitializeFunc: func(ctx context.Context, criuPid int32) (err error) {
				paths := manager.GetPaths()

				log.Trace().Msgf("restoring checkpoint from old cgroup to new cgroup\n")
				log.Trace().Msgf("CRIU process PID: %d\n", criuPid)
				log.Trace().Msgf("new cgroup paths:\n")
				for c, p := range paths {
					log.Trace().Msgf("  Controller %s: %s\n", c, p)
				}

				// ensure the new cgroup hierarchy exists
				for _, p := range paths {
					cgroupPath := filepath.Join("/sys/fs/cgroup", p)

					if _, statErr := os.Stat(cgroupPath); os.IsNotExist(statErr) {
						log.Trace().Msgf("creating cgroup path: %s\n", cgroupPath)
						if err := os.MkdirAll(cgroupPath, 0755); err != nil {
							return fmt.Errorf("failed to create cgroup path %s: %v", cgroupPath, err)
						}
					} else {
						log.Trace().Msgf("cgroup path already exists: %s\n", cgroupPath)
					}
				}

				// apply cgroups to the CRIU process
				err = manager.Apply(int(criuPid))
				if err != nil {
					return fmt.Errorf("failed to apply cgroups to CRIU process: %v", err)
				}
				log.Trace().Msgf("applied cgroups to CRIU process %d\n", criuPid)

				// set CgRoot to tell CRIU where to place restored processes
				for c, p := range paths {
					cgroupRoot := &criu_proto.CgroupRoot{
						Ctrl: proto.String(c),
						Path: proto.String(p),
					}
					req.Criu.CgRoot = append(req.Criu.CgRoot, cgroupRoot)
					log.Trace().Msgf("set CgRoot for controller %s: %s\n", c, p)
				}

				return nil
			},
		}
		opts.CRIUCallback.Include(callback)

		return next(ctx, opts, resp, req)
	}
}

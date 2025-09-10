package cgroup

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/pkg/criu"
	"github.com/cedana/cedana/pkg/types"
	runc_keys "github.com/cedana/cedana/plugins/runc/pkg/keys"
	"github.com/opencontainers/cgroups"
	"github.com/opencontainers/runc/libcontainer"
	"github.com/opencontainers/runc/libcontainer/configs"
	"github.com/rs/zerolog/log"
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

		var netPid int = 0
		if config.Namespaces != nil {
			for _, ns := range config.Namespaces {
				if ns.Type == configs.NEWNET && strings.Count(ns.Path, "/") > 1 {
					netpidStr := strings.Split(ns.Path, "/")[2]
					netPid, err = strconv.Atoi(netpidStr)
					if err != nil {
						return nil, status.Errorf(codes.Internal, "failed to parse network namespace PID: %v", err)
					}
					break
				}
			}
		}

		callback := &criu.NotifyCallback{
			InitializeFunc: func(ctx context.Context, criuPid int32) (err error) {
				var paths map[string]string

				if netPid != 0 { // Usually in k8s environments
					log.Debug().Int("netPid", netPid).Msg("will apply cgroups based on the network namespace PID (assuming k8s environment)")
					paths, err = cgroups.ParseCgroupFile(fmt.Sprintf("/proc/%d/cgroup", netPid))
					if err != nil {
						return fmt.Errorf("failed to parse cgroup file for net PID %d: %v", netPid, err)
					}
				} else { // Fallback
					err = manager.Apply(int(criuPid))
					if err != nil {
						return fmt.Errorf("failed to apply cgroups: %v", err)
					}
					err = manager.Set(config.Cgroups.Resources)
					if err != nil {
						return fmt.Errorf("failed to set cgroup resources: %v", err)
					}
					paths = manager.GetPaths()
				}

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

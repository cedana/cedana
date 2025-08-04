package cgroup

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/pkg/types"
	runc_keys "github.com/cedana/cedana/plugins/runc/pkg/keys"
	"github.com/opencontainers/cgroups"
	"github.com/opencontainers/runc/libcontainer"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

func UseCgroupFreezerIfAvailableForDump(next types.Dump) types.Dump {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (code func() <-chan int, err error) {
		manager, ok := ctx.Value(runc_keys.CONTAINER_CGROUP_MANAGER_CONTEXT_KEY).(cgroups.Manager)
		if !ok {
			return nil, status.Errorf(codes.FailedPrecondition, "failed to get cgroup manager from context")
		}

		container, ok := ctx.Value(runc_keys.CONTAINER_CONTEXT_KEY).(*libcontainer.Container)
		if !ok {
			return nil, status.Errorf(codes.FailedPrecondition, "failed to get container from context")
		}

		version, err := opts.CRIU.GetCriuVersion(ctx)
		if err != nil {
			return nil, status.Error(codes.Internal, fmt.Sprintf("failed to get CRIU version: %v", err))
		}

		if req.GetCriu() == nil {
			req.Criu = &criu_proto.CriuOpts{}
		}

		if !cgroups.IsCgroup2UnifiedMode() || version >= 31400 {
			log.Debug().Msg("using cgroup freezer for dump")

			// First try the manager's cached path, but verify it exists
			if fcg := manager.Path("freezer"); fcg != "" {
				// Check if the path looks like a valid filesystem path (no systemd unit format with colons)
				if !strings.Contains(fcg, ":") {
					// Verify the path exists before using it
					var checkFile string
					if cgroups.IsCgroup2UnifiedMode() {
						checkFile = filepath.Join(fcg, "cgroup.freeze")
					} else {
						checkFile = filepath.Join(fcg, "freezer.state")
					}
					if _, err := os.Stat(checkFile); err == nil {
						log.Debug().Str("path", fcg).Msg("using cached freezer cgroup path")
						req.Criu.FreezeCgroup = proto.String(fcg)
						return next(ctx, opts, resp, req)
					}
					log.Debug().Str("path", fcg).Str("checkFile", checkFile).Msg("cached freezer cgroup path does not exist, trying to find current path")
				} else {
					log.Debug().Str("path", fcg).Msg("cached freezer cgroup path is systemd unit format (contains colons), trying to find current filesystem path")
				}
			}

			// If cached path doesn't work, try to find the current cgroup path dynamically
			state, err := container.State()
			if err != nil {
				log.Debug().Err(err).Msg("failed to get container state, skipping cgroup freezer")
				return next(ctx, opts, resp, req)
			}

			// Read the current cgroup of the container's init process
			cgroupFile := fmt.Sprintf("/proc/%d/cgroup", state.InitProcessPid)
			cgroupPaths, err := cgroups.ParseCgroupFile(cgroupFile)
			if err != nil {
				log.Debug().Err(err).Msg("failed to parse current cgroup file, skipping cgroup freezer")
				return next(ctx, opts, resp, req)
			}

			log.Debug().Interface("cgroupPaths", cgroupPaths).Msg("parsed current cgroup paths")

			// Look for freezer cgroup path
			for controller, path := range cgroupPaths {
				var freezerPath string
				var checkPath string

				if cgroups.IsCgroup2UnifiedMode() {
					// cgroup v2: unified hierarchy
					freezerPath = filepath.Join("/sys/fs/cgroup", path)
					checkPath = filepath.Join(freezerPath, "cgroup.freeze")
				} else {
					// cgroup v1: check if this is the freezer controller or systemd unified
					if controller == "freezer" {
						freezerPath = filepath.Join("/sys/fs/cgroup/freezer", path)
						checkPath = filepath.Join(freezerPath, "freezer.state")
					} else if controller == "" || strings.Contains(controller, "systemd") {
						// systemd unified or empty controller
						freezerPath = filepath.Join("/sys/fs/cgroup", path)
						checkPath = filepath.Join(freezerPath, "freezer.state")
					} else {
						// skip non-freezer controllers
						continue
					}
				}

				log.Debug().Str("controller", controller).Str("path", path).Str("freezerPath", freezerPath).Str("checkPath", checkPath).Msg("checking cgroup path")

				if _, err := os.Stat(checkPath); err == nil {
					log.Debug().Str("path", freezerPath).Msg("found current freezer cgroup path")
					req.Criu.FreezeCgroup = proto.String(freezerPath)
					return next(ctx, opts, resp, req)
				}
			}

			log.Debug().Msg("no suitable freezer cgroup path found, continuing without freezer")
		}

		return next(ctx, opts, resp, req)
	}
}

package filesystem

import (
	"context"
	"os"
	"path/filepath"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/pkg/types"
	runc_keys "github.com/cedana/cedana/plugins/runc/pkg/keys"
	"github.com/cedana/cedana/plugins/runc/pkg/runc"
	"github.com/opencontainers/runc/libcontainer"
	"github.com/opencontainers/runc/libcontainer/cgroups"
	"github.com/opencontainers/runc/libcontainer/configs"
	libcontainer_utils "github.com/opencontainers/runc/libcontainer/utils"
	"github.com/rs/zerolog/log"
	"golang.org/x/sys/unix"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// CRIU has a few requirements for a root directory:
// * it must be a mount point
// * its parent must not be overmounted
// c.config.Rootfs is bind-mounted to a temporary directory
// to satisfy these requirements.
func MountRootDirForRestore(next types.Restore) types.Restore {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (exited chan int, err error) {
		container, ok := ctx.Value(runc_keys.CONTAINER_CONTEXT_KEY).(*libcontainer.Container)
		if !ok {
			return nil, status.Errorf(codes.FailedPrecondition, "failed to get container from context")
		}

		criuRoot := filepath.Join(os.TempDir(), "criu-root-"+container.ID())
		if err := os.Mkdir(criuRoot, 0o755); err != nil {
			return nil, status.Errorf(codes.Internal, "failed to create root directory for CRIU: %v", err)
		}
		defer os.RemoveAll(criuRoot)

		criuRoot, err = filepath.EvalSymlinks(criuRoot)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to resolve symlink: %v", err)
		}

		// Mount the rootfs
		err = runc.Mount(container.Config().Rootfs, criuRoot, "", unix.MS_BIND|unix.MS_REC, "")
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to mount rootfs: %v", err)
		}
		defer unix.Unmount(criuRoot, unix.MNT_DETACH)

		if req.Criu == nil {
			req.Criu = &criu_proto.CriuOpts{}
		}
		req.Criu.Root = proto.String(criuRoot)

		return next(ctx, opts, resp, req)
	}
}

// Tries to set up the rootfs of the
// container to be restored in the same way runc does it for
// initial container creation. Even for a read-only rootfs container
// runc modifies the rootfs to add mountpoints which do not exist.
// This function also creates missing mountpoints as long as they
// are not on top of a tmpfs, as CRIU will restore tmpfs content anyway.
func SetupMountsForRestore(next types.Restore) types.Restore {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (exited chan int, err error) {
		container, ok := ctx.Value(runc_keys.CONTAINER_CONTEXT_KEY).(*libcontainer.Container)
		if !ok {
			return nil, status.Errorf(codes.FailedPrecondition, "failed to get container from context")
		}

		config := container.Config()

		// First get a list of a all tmpfs mounts
		tmpfs := []string{}
		for _, m := range config.Mounts {
			switch m.Device {
			case "tmpfs":
				tmpfs = append(tmpfs, m.Destination)
			}
		}

		// Go through all mounts and create the mountpoints
		// if the mountpoints are not on a tmpfs, as CRIU will
		// restore the complete tmpfs content from its checkpoint.

		umounts := []string{}
		defer func() {
			for _, u := range umounts {
				_ = libcontainer_utils.WithProcfd(config.Rootfs, u, func(procfd string) error {
					if e := unix.Unmount(procfd, unix.MNT_DETACH); e != nil {
						if e != unix.EINVAL {
							// Ignore EINVAL as it means 'target is not a mount point.'
							// It probably has already been unmounted.
							log.Warn().Msgf("Error during cleanup unmounting of %s (%s): %v", procfd, u, e)
						}
					}
					return nil
				})
			}
		}()

		for _, m := range config.Mounts {
			if !IsPathInPrefixList(m.Destination, tmpfs) {
				if m.Device == "cgroup" {
					// No mount point(s) need to be created:
					//
					// * for v1, mount points are saved by CRIU because
					//   /sys/fs/cgroup is a tmpfs mount
					//
					// * for v2, /sys/fs/cgroup is a real mount, but
					//   the mountpoint appears as soon as /sys is mounted
					continue
				}

				me := runc.MountEntry{Mount: m}
				// For all other filesystems, just make the target.
				if _, err := runc.CreateMountpoint(config.Rootfs, me); err != nil {
					return nil, status.Errorf(codes.Internal, "failed to create mountpoint for %s: %v", m.Destination, err)
				}
				// If the mount point is a bind mount, we need to mount
				// it now so that runc can create the necessary mount
				// points for mounts in bind mounts.
				// This also happens during initial container creation.
				// Without this CRIU restore will fail
				// See: https://github.com/opencontainers/runc/issues/2748
				// It is also not necessary to order the mount points
				// because during initial container creation mounts are
				// set up in the order they are configured.
				if m.Device == "bind" {
					if err := libcontainer_utils.WithProcfd(config.Rootfs, m.Destination, func(dstFd string) error {
						return runc.MountViaFds(m.Source, nil, m.Destination, dstFd, "", unix.MS_BIND|unix.MS_REC, "")
					}); err != nil {
						return nil, status.Errorf(codes.Internal, "failed to bind mount %s: %v", m.Destination, err)
					}
					umounts = append(umounts, m.Destination)
				}
			}
		}

		return next(ctx, opts, resp, req)
	}
}

func AddMountsForRestore(next types.Restore) types.Restore {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (chan int, error) {
		container, ok := ctx.Value(runc_keys.CONTAINER_CONTEXT_KEY).(*libcontainer.Container)
		if !ok {
			return nil, status.Errorf(codes.FailedPrecondition, "failed to get container from context")
		}

		if req.Criu == nil {
			req.Criu = &criu_proto.CriuOpts{}
		}

		config := container.Config()

		hasCgroupns := config.Namespaces.Contains(configs.NEWCGROUP)
		for _, m := range config.Mounts {
			switch m.Device {
			case "bind":
				CriuAddExternalMount(req.Criu, m, config.Rootfs)
			case "cgroup":
				if cgroups.IsCgroup2UnifiedMode() || hasCgroupns {
					continue
				}
				// cgroup v1 is a set of bind mounts, unless cgroupns is used
				binds, err := GetCgroupMounts(m)
				if err != nil {
					return nil, status.Errorf(codes.Internal, "failed to get cgroup mounts: %v", err)
				}
				for _, b := range binds {
					CriuAddExternalMount(req.Criu, b, config.Rootfs)
				}
			}
		}

		return next(ctx, opts, resp, req)
	}
}

func AddMaskedPathsForRestore(next types.Restore) types.Restore {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (chan int, error) {
		container, ok := ctx.Value(runc_keys.CONTAINER_CONTEXT_KEY).(*libcontainer.Container)
		if !ok {
			return nil, status.Errorf(codes.FailedPrecondition, "failed to get container from context")
		}

		if req.Criu == nil {
			req.Criu = &criu_proto.CriuOpts{}
		}

		config := container.Config()

		if len(config.MaskPaths) > 0 {
			m := &configs.Mount{Destination: "/dev/null", Source: "/dev/null"}
			CriuAddExternalMount(req.Criu, m, config.Rootfs)
		}

		return next(ctx, opts, resp, req)
	}
}

package adapters

import (
	"context"
	"path/filepath"
	"strings"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/pkg/criu"
	"github.com/cedana/cedana/pkg/types"
	runc_keys "github.com/cedana/cedana/plugins/runc/pkg/keys"
	securejoin "github.com/cyphar/filepath-securejoin"
	"github.com/opencontainers/runc/libcontainer"
	"github.com/opencontainers/runc/libcontainer/cgroups"
	"github.com/opencontainers/runc/libcontainer/configs"
	"golang.org/x/sys/unix"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// Defines all the adapters that manage devices / device mounts

///////////////////////
//// Dump Adapters ////
///////////////////////

func AddDeviceMountsForDump(next types.Dump) types.Dump {
	return func(ctx context.Context, server types.ServerOpts, nfy *criu.NotifyCallbackMulti, resp *daemon.DumpResp, req *daemon.DumpReq) (chan int, error) {
		container, ok := ctx.Value(runc_keys.DUMP_CONTAINER_CONTEXT_KEY).(*libcontainer.Container)
		if !ok {
			return nil, status.Errorf(
				codes.FailedPrecondition,
				"failed to get container from context",
			)
		}

		criuOpts := req.GetCriu()
		if criuOpts == nil {
			criuOpts = &criu_proto.CriuOpts{}
		}

		// TODO: return early if pre-dump, as we don't do all of this for pre-dump

		config := container.Config()
		rootfs := config.Rootfs

		for _, m := range container.Config().Mounts {
			hasCgroupns := config.Namespaces.Contains(configs.NEWCGROUP)
			switch m.Device {
			case "bind":
				criuAddExternalMount(criuOpts, m, rootfs)
			case "cgroup":
				if cgroups.IsCgroup2UnifiedMode() || hasCgroupns {
					// real mount(s)
					continue
				}
				// a set of "external" bind mounts
				binds, err := getCgroupMounts(m)
				if err != nil {
					return nil, status.Errorf(
						codes.Internal,
						"failed to get cgroup mounts: %v",
						err,
					)
				}
				for _, b := range binds {
					criuAddExternalMount(criuOpts, b, rootfs)
				}
			}
		}

		for _, d := range config.Devices {
			m := &configs.Mount{Destination: d.Path, Source: d.Path}
			criuAddExternalMount(criuOpts, m, rootfs)
		}

		return next(ctx, server, nfy, resp, req)
	}
}

//////////////////////////
//// Restore Adapters ////
//////////////////////////

//////////////////////////
//// Helper Functions ////
//////////////////////////

// lifted from libcontainer
func criuAddExternalMount(opts *criu_proto.CriuOpts, m *configs.Mount, rootfs string) {
	mountDest := strings.TrimPrefix(m.Destination, rootfs)
	if dest, err := securejoin.SecureJoin(rootfs, mountDest); err == nil {
		mountDest = dest[len(rootfs):]
	}
	extMnt := &criu_proto.ExtMountMap{
		Key: proto.String(mountDest),
		Val: proto.String(mountDest),
	}
	opts.ExtMnt = append(opts.ExtMnt, extMnt)
}

// lifted from libcontainer
func getCgroupMounts(m *configs.Mount) ([]*configs.Mount, error) {
	mounts, err := cgroups.GetCgroupMounts(false)
	if err != nil {
		return nil, err
	}

	// We don't need to use /proc/thread-self here because runc always runs
	// with every thread in the same cgroup. This lets us avoid having to do
	// runtime.LockOSThread.
	cgroupPaths, err := cgroups.ParseCgroupFile("/proc/self/cgroup")
	if err != nil {
		return nil, err
	}

	var binds []*configs.Mount

	for _, mm := range mounts {
		dir, err := mm.GetOwnCgroup(cgroupPaths)
		if err != nil {
			return nil, err
		}
		relDir, err := filepath.Rel(mm.Root, dir)
		if err != nil {
			return nil, err
		}
		binds = append(binds, &configs.Mount{
			Device:           "bind",
			Source:           filepath.Join(mm.Mountpoint, relDir),
			Destination:      filepath.Join(m.Destination, filepath.Base(mm.Mountpoint)),
			Flags:            unix.MS_BIND | unix.MS_REC | m.Flags,
			PropagationFlags: m.PropagationFlags,
		})
	}

	return binds, nil
}

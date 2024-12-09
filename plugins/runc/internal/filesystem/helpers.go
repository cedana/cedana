package filesystem

import (
	"path/filepath"
	"strings"

	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	securejoin "github.com/cyphar/filepath-securejoin"
	"github.com/opencontainers/runc/libcontainer/cgroups"
	"github.com/opencontainers/runc/libcontainer/configs"
	"golang.org/x/sys/unix"
	"google.golang.org/protobuf/proto"
)

// IsPathInPrefixList is a small function for CRIU restore to make sure
// mountpoints, which are on a tmpfs, are not created in the roofs.
func IsPathInPrefixList(path string, prefix []string) bool {
	for _, p := range prefix {
		if strings.HasPrefix(path, p+"/") {
			return true
		}
	}
	return false
}

// lifted from libcontainer
func CriuAddExternalMount(opts *criu_proto.CriuOpts, m *configs.Mount, rootfs string) {
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
func GetCgroupMounts(m *configs.Mount) ([]*configs.Mount, error) {
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

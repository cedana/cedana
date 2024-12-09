package runc

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	securejoin "github.com/cyphar/filepath-securejoin"
	"github.com/rs/zerolog/log"
	"golang.org/x/sys/unix"

	"github.com/opencontainers/runc/libcontainer/configs"
	"github.com/opencontainers/runc/libcontainer/utils"
)

// Taken from <include/linux/proc_ns.h>. If a file is on a filesystem of type
// PROC_SUPER_MAGIC, we're guaranteed that only the root of the superblock will
// have this inode number.
const procRootIno = 1

var errRootfsToFile = errors.New("config tries to change rootfs to file")

// MountEntry contains mount data specific to a mount point.
type MountEntry struct {
	*configs.Mount
	srcFile *mountSource
}

// srcName is only meant for error messages, it returns a "friendly" name.
func (m MountEntry) srcName() string {
	if m.srcFile != nil {
		return m.srcFile.file.Name()
	}
	return m.Source
}

func (m MountEntry) srcStat() (os.FileInfo, *syscall.Stat_t, error) {
	var (
		st  os.FileInfo
		err error
	)
	if m.srcFile != nil {
		st, err = m.srcFile.file.Stat()
	} else {
		st, err = os.Stat(m.Source)
	}
	if err != nil {
		return nil, nil, err
	}
	return st, st.Sys().(*syscall.Stat_t), nil
}

func (m MountEntry) srcStatfs() (*unix.Statfs_t, error) {
	var st unix.Statfs_t
	if m.srcFile != nil {
		if err := unix.Fstatfs(int(m.srcFile.file.Fd()), &st); err != nil {
			return nil, os.NewSyscallError("fstatfs", err)
		}
	} else {
		if err := unix.Statfs(m.Source, &st); err != nil {
			return nil, &os.PathError{Op: "statfs", Path: m.Source, Err: err}
		}
	}
	return &st, nil
}

// checkProcMount checks to ensure that the mount destination is not over the top of /proc.
// dest is required to be an abs path and have any symlinks resolved before calling this function.
//
// If m is nil, don't stat the filesystem.  This is used for restore of a checkpoint.
func checkProcMount(rootfs, dest string, m MountEntry) error {
	const procPath = "/proc"
	path, err := filepath.Rel(filepath.Join(rootfs, procPath), dest)
	if err != nil {
		return err
	}
	// pass if the mount path is located outside of /proc
	if strings.HasPrefix(path, "..") {
		return nil
	}
	if path == "." {
		// Only allow bind-mounts on top of /proc, and only if the source is a
		// procfs mount.
		if m.IsBind() {
			fsSt, err := m.srcStatfs()
			if err != nil {
				return err
			}
			if fsSt.Type == unix.PROC_SUPER_MAGIC {
				if _, uSt, err := m.srcStat(); err != nil {
					return err
				} else if uSt.Ino != procRootIno {
					// We cannot error out in this case, because we've
					// supported these kinds of mounts for a long time.
					// However, we would expect users to bind-mount the root of
					// a real procfs on top of /proc in the container. We might
					// want to block this in the future.
					log.Warn().Msgf("bind-mount %v (source %v) is of type procfs but is not the root of a procfs (inode %d). Future versions of runc might block this configuration -- please report an issue to <https://github.com/opencontainers/runc> if you see this warning.", dest, m.srcName(), uSt.Ino)
				}
				return nil
			}
		} else if m.Device == "proc" {
			// Fresh procfs-type mounts are always safe to mount on top of /proc.
			return nil
		}
		return fmt.Errorf("%q cannot be mounted because it is not of type proc", dest)
	}

	// Here dest is definitely under /proc. Do not allow those,
	// except for a few specific entries emulated by lxcfs.
	validProcMounts := []string{
		"/proc/cpuinfo",
		"/proc/diskstats",
		"/proc/meminfo",
		"/proc/stat",
		"/proc/swaps",
		"/proc/uptime",
		"/proc/loadavg",
		"/proc/slabinfo",
		"/proc/net/dev",
		"/proc/sys/kernel/ns_last_pid",
		"/proc/sys/crypto/fips_enabled",
	}
	for _, valid := range validProcMounts {
		path, err := filepath.Rel(filepath.Join(rootfs, valid), dest)
		if err != nil {
			return err
		}
		if path == "." {
			return nil
		}
	}

	return fmt.Errorf("%q cannot be mounted because it is inside /proc", dest)
}

func CreateMountpoint(rootfs string, m MountEntry) (string, error) {
	dest, err := securejoin.SecureJoin(rootfs, m.Destination)
	if err != nil {
		return "", err
	}
	if err := checkProcMount(rootfs, dest, m); err != nil {
		return "", fmt.Errorf("check proc-safety of %s mount: %w", m.Destination, err)
	}

	switch m.Device {
	case "bind":
		fi, _, err := m.srcStat()
		if err != nil {
			// Error out if the source of a bind mount does not exist as we
			// will be unable to bind anything to it.
			return "", err
		}
		// If the original source is not a directory, make the target a file.
		if !fi.IsDir() {
			// Make sure we aren't tricked into trying to make the root a file.
			if rootfs == dest {
				return "", fmt.Errorf("%w: file bind mount over rootfs", errRootfsToFile)
			}
			// Make the parent directory.
			destDir, destBase := filepath.Split(dest)
			destDirFd, err := utils.MkdirAllInRootOpen(rootfs, destDir, 0o755)
			if err != nil {
				return "", fmt.Errorf("make parent dir of file bind-mount: %w", err)
			}
			defer destDirFd.Close()
			// Make the target file. We want to avoid opening any file that is
			// already there because it could be a "bad" file like an invalid
			// device or hung tty that might cause a DoS, so we use mknodat.
			// destBase does not contain any "/" components, and mknodat does
			// not follow trailing symlinks, so we can safely just call mknodat
			// here.
			if err := unix.Mknodat(int(destDirFd.Fd()), destBase, unix.S_IFREG|0o644, 0); err != nil {
				// If we get EEXIST, there was already an inode there and
				// we can consider that a success.
				if !errors.Is(err, unix.EEXIST) {
					err = &os.PathError{Op: "mknod regular file", Path: dest, Err: err}
					return "", fmt.Errorf("create target of file bind-mount: %w", err)
				}
			}
			// Nothing left to do.
			return dest, nil
		}

	case "tmpfs":
		// If the original target exists, copy the mode for the tmpfs mount.
		if stat, err := os.Stat(dest); err == nil {
			dt := fmt.Sprintf("mode=%04o", syscallMode(stat.Mode()))
			if m.Data != "" {
				dt = dt + "," + m.Data
			}
			m.Data = dt

			// Nothing left to do.
			return dest, nil
		}
	}

	if err := utils.MkdirAllInRoot(rootfs, dest, 0o755); err != nil {
		return "", err
	}
	return dest, nil
}

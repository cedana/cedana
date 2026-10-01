//go:build linux

package filesystem

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/rs/zerolog/log"
	"golang.org/x/sys/unix"
)

const xattrPrefix = "SCHILY.xattr."

// walkMount walks dir, itself included, without descending into other filesystems mounted below it
func walkMount(dir string, fn func(path string, info fs.FileInfo, st *syscall.Stat_t) error) error {
	dir = filepath.Clean(dir)
	var rootStat unix.Stat_t
	if err := unix.Stat(dir, &rootStat); err != nil {
		return fmt.Errorf("failed to stat %s: %w", dir, err)
	}

	return filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		info, err := d.Info()
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			return fmt.Errorf("no stat for %s", path)
		}
		if isOurs(dir, path) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if uint64(st.Dev) != uint64(rootStat.Dev) {
			log.Warn().Str("path", path).Msg("not archiving another filesystem mounted inside")
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		return fn(path, info, st)
	})
}

// contentSize is how much of file contents archiveDir would write for dir. It's the size files
// appear to have, which for a sparse file can be far more than what it takes up.
func contentSize(dir string) (uint64, error) {
	var size uint64
	seen := map[uint64]bool{} // inodes with more than one name

	err := walkMount(dir, func(path string, info fs.FileInfo, st *syscall.Stat_t) error {
		if !info.Mode().IsRegular() {
			return nil
		}
		if st.Nlink > 1 {
			if seen[st.Ino] {
				return nil
			}
			seen[st.Ino] = true
		}
		size += uint64(info.Size())
		return nil
	})

	return size, err
}

// archiveDir writes dir as a tar, with ownership, modes, times and xattrs. Does not descend into
// other filesystems mounted below dir. Files with more than one name (hardlinks) stay that way.
// Anything that is not a regular file, directory, symlink or FIFO is skipped: a socket comes
// with the process bound to it, and a device can't be made in a tmpfs that is nodev.
//
// Fails before writing more than max bytes of file contents. Returns how many were written.
func archiveDir(dir string, w io.Writer, max uint64) (written uint64, err error) {
	tw := tar.NewWriter(w)
	links := map[uint64]string{} // inode -> first name archived, for those with more than one

	err = walkMount(dir, func(path string, info fs.FileInfo, st *syscall.Stat_t) error {
		mode := info.Mode()
		link := ""
		switch {
		case mode.IsRegular(), mode.IsDir(), mode&fs.ModeNamedPipe != 0:
		case mode&fs.ModeSymlink != 0:
			if link, err = os.Readlink(path); err != nil {
				return err
			}
		default:
			log.Warn().Str("path", path).Str("mode", mode.String()).Msg("not archiving special file")
			return nil
		}

		header, err := tar.FileInfoHeader(info, link)
		if err != nil {
			return err
		}
		header.Format = tar.FormatPAX
		header.Name, err = filepath.Rel(dir, path) // dir itself is '.', for what it is owned by and its mode
		if err != nil {
			return err
		}
		header.Uid, header.Gid = int(st.Uid), int(st.Gid)
		header.Uname, header.Gname = "", "" // names mean nothing on another node, IDs are what CRIU restores with

		if mode.IsRegular() && st.Nlink > 1 {
			if first, ok := links[st.Ino]; ok {
				header.Typeflag = tar.TypeLink
				header.Linkname = first
				header.Size = 0
				return tw.WriteHeader(header)
			}
			links[st.Ino] = header.Name
		}

		if mode.IsRegular() || mode.IsDir() {
			xattrs, err := readXattrs(path)
			if err != nil {
				return fmt.Errorf("failed to read xattrs of %s: %w", path, err)
			}
			for name, value := range xattrs {
				if header.PAXRecords == nil {
					header.PAXRecords = map[string]string{}
				}
				header.PAXRecords[xattrPrefix+name] = value
			}
		}

		if !mode.IsRegular() {
			return tw.WriteHeader(header)
		}

		if size := uint64(header.Size); size > max-written {
			return fmt.Errorf("%s brings the contents to more than the %d bytes allowed", path, max)
		}
		if err := tw.WriteHeader(header); err != nil {
			return err
		}

		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()

		// The job is frozen, but be exact about the size promised in the header regardless
		n, err := io.CopyN(tw, file, header.Size)
		written += uint64(n)
		if err != nil {
			return fmt.Errorf("failed to archive %s: %w", path, err)
		}
		return nil
	})
	if err != nil {
		return written, err
	}

	return written, tw.Close()
}

// extractDir unpacks a tar made by archiveDir into dir, which must exist.
// Nothing can be written outside dir, whatever the archive or existing symlinks in dir say,
// nor into another filesystem mounted below it.
func extractDir(r io.Reader, dir string) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()

	device, err := deviceOf(root, ".")
	if err != nil {
		return err
	}

	// archiveDir leaves out what is mounted below, so there's nothing of it in the archive. If
	// the archive has something to put there, it's not the same mount (e.g. shared with the host).
	inside := map[string]bool{".": true}
	checkInside := func(name string) error {
		if inside[name] {
			return nil
		}
		dev, err := deviceOf(root, name)
		if err != nil {
			return err
		}
		if dev != device {
			return fmt.Errorf("%s is on another filesystem mounted inside", name)
		}
		inside[name] = true
		return nil
	}

	type dirMeta struct {
		name  string
		mode  os.FileMode
		atime time.Time
		mtime time.Time
	}
	// Applied last, deepest first: the mode may not let us create anything inside,
	// and creating files inside changes the times
	var dirs []dirMeta

	tr := tar.NewReader(r)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		name, ok := cleanName(header.Name)
		if !ok || (name == "." && header.Typeflag != tar.TypeDir) {
			return fmt.Errorf("invalid path %q in archive", header.Name)
		}
		mode := header.FileInfo().Mode()

		if name != "." {
			if err := checkInside(filepath.Dir(name)); err != nil {
				return err
			}
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if name != "." {
				err := root.Mkdir(name, 0o700)
				if errors.Is(err, fs.ErrExist) {
					// Could be a symlink to elsewhere, or where something else is mounted
					err = checkInside(name)
				}
				if err != nil {
					return err
				}
			}
			dirs = append(dirs, dirMeta{name, mode, header.AccessTime, header.ModTime})

		case tar.TypeReg:
			// Never write through whatever is already there by this name
			if err := root.Remove(name); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return err
			}
			file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
			if err != nil {
				return err
			}
			_, err = io.Copy(file, tr)
			if cerr := file.Close(); err == nil {
				err = cerr
			}
			if err != nil {
				return fmt.Errorf("failed to extract %s: %w", name, err)
			}

		case tar.TypeFifo:
			if err := root.Remove(name); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return err
			}
			if err := mkfifo(root, name); err != nil {
				return fmt.Errorf("failed to make FIFO %s: %w", name, err)
			}

		case tar.TypeLink:
			target, ok := cleanName(header.Linkname)
			if !ok || target == "." {
				return fmt.Errorf("invalid link %q to %q in archive", header.Name, header.Linkname)
			}
			if err := root.Remove(name); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return err
			}
			if err := root.Link(target, name); err != nil {
				return err
			}
			continue // everything else is that of the file linked to

		case tar.TypeSymlink:
			if err := root.Remove(name); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return err
			}
			if err := root.Symlink(header.Linkname, name); err != nil {
				return err
			}
			if err := setOwner(root, name, header.Uid, header.Gid); err != nil {
				return err
			}
			continue

		default:
			log.Warn().Str("path", name).Msgf("not extracting entry of type %q", header.Typeflag)
			continue
		}

		// chown clears setuid/setgid and capabilities, so mode and xattrs go after
		if err := setOwner(root, name, header.Uid, header.Gid); err != nil {
			return err
		}
		if header.Typeflag != tar.TypeFifo { // has to be opened for it, which a FIFO waits on
			if err := writeXattrs(root, name, header.PAXRecords); err != nil {
				return fmt.Errorf("failed to write xattrs of %s: %w", name, err)
			}
		}
		if header.Typeflag == tar.TypeDir {
			continue
		}
		if err := setMode(root, name, mode); err != nil {
			return err
		}
		if err := setTimes(root, name, header.AccessTime, header.ModTime); err != nil {
			return err
		}
	}

	for i := len(dirs) - 1; i >= 0; i-- {
		d := dirs[i]
		if err := setMode(root, d.name, d.mode); err != nil {
			return err
		}
		if err := setTimes(root, d.name, d.atime, d.mtime); err != nil {
			return err
		}
	}

	return nil
}

// cleanName returns the path of an archive entry, if it's one that stays inside
func cleanName(name string) (string, bool) {
	name = filepath.Clean(name)
	if name == "" || name == ".." || filepath.IsAbs(name) || strings.HasPrefix(name, "../") {
		return "", false
	}
	return name, true
}

// deviceOf is for directories only, and a symlink to one is not
func deviceOf(root *os.Root, name string) (uint64, error) {
	info, err := root.Lstat(name)
	if err != nil {
		return 0, err
	}
	if !info.IsDir() {
		return 0, fmt.Errorf("%s is not a directory", name)
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, fmt.Errorf("no stat for %s", name)
	}
	return uint64(st.Dev), nil
}

// mkfifo is what os.Root has no way of doing. With only the last component of name
// left to resolve from its directory, there's no way outside here either.
func mkfifo(root *os.Root, name string) error {
	parent, err := root.Open(filepath.Dir(name))
	if err != nil {
		return err
	}
	defer parent.Close()

	return unix.Mknodat(int(parent.Fd()), filepath.Base(name), unix.S_IFIFO|0o600, 0)
}

func readXattrs(path string) (map[string]string, error) {
	size, err := unix.Llistxattr(path, nil)
	if err != nil {
		if errors.Is(err, unix.ENOTSUP) {
			return nil, nil
		}
		return nil, err
	}
	if size == 0 {
		return nil, nil
	}
	list := make([]byte, size)
	if size, err = unix.Llistxattr(path, list); err != nil {
		return nil, err
	}

	xattrs := map[string]string{}
	for _, name := range strings.Split(strings.TrimRight(string(list[:size]), "\x00"), "\x00") {
		if name == "" {
			continue
		}
		size, err := unix.Lgetxattr(path, name, nil)
		if err != nil {
			if errors.Is(err, unix.ENODATA) {
				continue
			}
			return nil, err
		}
		value := make([]byte, size)
		if size, err = unix.Lgetxattr(path, name, value); err != nil {
			return nil, err
		}
		xattrs[name] = string(value[:size])
	}

	return xattrs, nil
}

func writeXattrs(root *os.Root, name string, records map[string]string) error {
	var file *os.File
	defer func() {
		if file != nil {
			file.Close()
		}
	}()

	for key, value := range records {
		xattr, ok := strings.CutPrefix(key, xattrPrefix)
		if !ok {
			continue
		}
		if file == nil {
			var err error
			if file, err = root.Open(name); err != nil {
				return err
			}
		}
		if err := unix.Fsetxattr(int(file.Fd()), xattr, []byte(value), 0); err != nil {
			if notAllowed(err, name, "xattr "+xattr) {
				continue
			}
			return fmt.Errorf("%s: %w", xattr, err)
		}
	}

	return nil
}

// Restoring as the job's user, rather than as root, the root of the mount is the launcher's,
// made and owned by it, and not ours to change. That one is left as it is, with a warning.
// Everything inside was archived from the job and has to come back as it was, so not being
// allowed to make it so fails the restore, as any other error.

// notAllowed tells whether err is us not being allowed to set what we may leave, and says so
func notAllowed(err error, name, what string) bool {
	if name != "." || !errors.Is(err, fs.ErrPermission) {
		return false
	}
	log.Warn().Err(err).Str("path", name).Msgf("not allowed to set the %s of the mount's root, leaving it as it is", what)
	return true
}

// setOwner gives name to uid:gid, unless it is theirs already
func setOwner(root *os.Root, name string, uid, gid int) error {
	info, err := root.Lstat(name)
	if err != nil {
		return err
	}
	if st, ok := info.Sys().(*syscall.Stat_t); ok && int(st.Uid) == uid && int(st.Gid) == gid {
		return nil
	}
	if err := root.Lchown(name, uid, gid); err != nil && !notAllowed(err, name, "owner") {
		return err
	}
	return nil
}

// setMode sets the permission and special bits of name, unless they are those already
func setMode(root *os.Root, name string, mode os.FileMode) error {
	const bits = fs.ModePerm | fs.ModeSetuid | fs.ModeSetgid | fs.ModeSticky
	info, err := root.Lstat(name)
	if err != nil {
		return err
	}
	if info.Mode()&bits == mode&bits {
		return nil
	}
	if err := root.Chmod(name, mode); err != nil && !notAllowed(err, name, "mode") {
		return err
	}
	return nil
}

// setTimes sets the times of name
func setTimes(root *os.Root, name string, atime, mtime time.Time) error {
	if err := root.Chtimes(name, atime, mtime); err != nil && !notAllowed(err, name, "times") {
		return err
	}
	return nil
}

// What the GPU controller keeps in the job's /dev/shm: its shared memory, the size of
// gpu.shm_size, and what goes with it. Named like the files in internal/cedana/gpu.
const gpuControllerPrefix = "cedana-gpu."

// isOurs tells an entry at the root of the mount that is cedana's own rather than the job's.
// The GPU controller's shared memory is dumped and restored with the GPU, not as a file,
// and would take up most of what a dump allows.
func isOurs(dir, path string) bool {
	return filepath.Dir(path) == dir && strings.HasPrefix(filepath.Base(path), gpuControllerPrefix)
}

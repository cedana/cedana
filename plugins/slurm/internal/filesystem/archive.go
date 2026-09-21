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

// archiveDir writes the contents of dir (but not dir itself) as a tar, with ownership, modes,
// times and xattrs. Does not descend into other filesystems mounted below dir.
// Anything that is not a regular file, directory or symlink is skipped.
func archiveDir(dir string, w io.Writer) error {
	var rootStat unix.Stat_t
	if err := unix.Stat(dir, &rootStat); err != nil {
		return fmt.Errorf("failed to stat %s: %w", dir, err)
	}

	tw := tar.NewWriter(w)

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == dir {
			return nil
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
		if uint64(st.Dev) != uint64(rootStat.Dev) {
			log.Warn().Str("path", path).Msg("not archiving another filesystem mounted inside")
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		mode := info.Mode()
		link := ""
		switch {
		case mode.IsRegular(), mode.IsDir():
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
		header.Name, err = filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		header.Uid, header.Gid = int(st.Uid), int(st.Gid)
		header.Uname, header.Gname = "", "" // names mean nothing on another node, IDs are what CRIU restores with

		if mode&fs.ModeSymlink == 0 {
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

		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		if !mode.IsRegular() {
			return nil
		}

		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()

		// The job is frozen, but be exact about the size promised in the header regardless
		if _, err := io.CopyN(tw, file, header.Size); err != nil {
			return fmt.Errorf("failed to archive %s: %w", path, err)
		}
		return nil
	})
	if err != nil {
		return err
	}

	return tw.Close()
}

// extractDir unpacks a tar made by archiveDir into dir, which must exist.
// Nothing can be written outside dir, whatever the archive or existing symlinks in dir say.
func extractDir(r io.Reader, dir string) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()

	type dirTimes struct {
		name  string
		atime time.Time
		mtime time.Time
	}
	var dirs []dirTimes // applied last, as creating files inside changes them

	tr := tar.NewReader(r)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		name := filepath.Clean(header.Name)
		if name == "." || name == ".." || filepath.IsAbs(name) || strings.HasPrefix(name, "../") {
			return fmt.Errorf("invalid path %q in archive", header.Name)
		}
		mode := header.FileInfo().Mode()

		switch header.Typeflag {
		case tar.TypeDir:
			if err := root.Mkdir(name, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
				return err
			}
			dirs = append(dirs, dirTimes{name, header.AccessTime, header.ModTime})

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

		case tar.TypeSymlink:
			if err := root.Remove(name); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return err
			}
			if err := root.Symlink(header.Linkname, name); err != nil {
				return err
			}
			if err := root.Lchown(name, header.Uid, header.Gid); err != nil {
				return err
			}
			continue

		default:
			log.Warn().Str("path", name).Msgf("not extracting entry of type %q", header.Typeflag)
			continue
		}

		// chown clears setuid/setgid, so mode goes after
		if err := root.Lchown(name, header.Uid, header.Gid); err != nil {
			return err
		}
		if err := root.Chmod(name, mode); err != nil {
			return err
		}
		if err := writeXattrs(root, name, header.PAXRecords); err != nil {
			return fmt.Errorf("failed to write xattrs of %s: %w", name, err)
		}
		if header.Typeflag == tar.TypeReg {
			if err := root.Chtimes(name, header.AccessTime, header.ModTime); err != nil {
				return err
			}
		}
	}

	for i := len(dirs) - 1; i >= 0; i-- {
		if err := root.Chtimes(dirs[i].name, dirs[i].atime, dirs[i].mtime); err != nil {
			return err
		}
	}

	return nil
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
			return fmt.Errorf("%s: %w", xattr, err)
		}
	}

	return nil
}

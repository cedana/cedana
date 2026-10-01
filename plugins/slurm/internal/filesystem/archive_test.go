//go:build linux

package filesystem

import (
	"archive/tar"
	"bytes"
	"errors"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/spf13/afero"
	"golang.org/x/sys/unix"
)

const noLimit = math.MaxUint64

func TestArchiveExtractDir(t *testing.T) {
	// For the modes to be what they're created with
	defer syscall.Umask(syscall.Umask(0))

	src := t.TempDir()
	mtime := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(src, "a/b"), 0o750))
	must(os.WriteFile(filepath.Join(src, "a/b/data"), []byte("hello"), 0o640))
	must(os.WriteFile(filepath.Join(src, "empty"), nil, 0o600))
	must(os.WriteFile(filepath.Join(src, "sticky"), []byte("x"), 0o644))
	must(os.Chmod(filepath.Join(src, "sticky"), 0o644|os.ModeSetgid))
	must(os.Symlink("a/b/data", filepath.Join(src, "link")))
	must(os.Symlink("/etc/passwd", filepath.Join(src, "abs")))
	must(os.Link(filepath.Join(src, "a/b/data"), filepath.Join(src, "hardlink")))
	must(unix.Mkfifo(filepath.Join(src, "fifo"), 0o620))
	// The GPU controller's, at the root of the mount: not the job's to archive. Deeper
	// down, a name like it is the job's.
	must(os.WriteFile(filepath.Join(src, "cedana-gpu.0123"), make([]byte, 4096), 0o600))
	must(os.MkdirAll(filepath.Join(src, "cedana-gpu.0123.misc"), 0o700))
	must(os.WriteFile(filepath.Join(src, "cedana-gpu.0123.misc/hostmem-0"), make([]byte, 4096), 0o600))
	must(os.WriteFile(filepath.Join(src, "a/cedana-gpu.4567"), []byte("job"), 0o600))
	// An xattr, and a capability if we may set one. Neither survives a chown of the file.
	if err := unix.Setxattr(filepath.Join(src, "sticky"), "user.test", []byte("value"), 0); err != nil && !errors.Is(err, unix.ENOTSUP) {
		t.Fatal(err)
	}
	capability := []byte{1, 0, 0, 2, 0, 4, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0} // NET_BIND_SERVICE, effective
	if err := unix.Setxattr(filepath.Join(src, "a/b/data"), "security.capability", capability, 0); err != nil && !errors.Is(err, unix.EPERM) {
		t.Fatal(err)
	}
	must(os.Chtimes(filepath.Join(src, "a/b/data"), mtime, mtime))
	must(os.Chtimes(filepath.Join(src, "a"), mtime, mtime))
	must(os.Chmod(src, 0o770|os.ModeSetgid|os.ModeSticky))
	must(os.Chtimes(src, mtime, mtime))
	// Nothing can be created in it once it has this mode
	must(os.Chmod(filepath.Join(src, "a/b"), 0o500))
	t.Cleanup(func() { os.Chmod(filepath.Join(src, "a/b"), 0o750) })

	var buf bytes.Buffer
	written, err := archiveDir(src, &buf, noLimit)
	must(err)
	if written != 9 { // "hello" once, "x", and "job"; nothing of the GPU controller's
		t.Errorf("expected 9 bytes of contents, got %d", written)
	}
	if size, err := contentSize(src); err != nil || size != written {
		t.Errorf("expected a size of %d, got %d, %v", written, size, err)
	}

	dst := t.TempDir()
	t.Cleanup(func() { os.Chmod(filepath.Join(dst, "a/b"), 0o750) })
	must(os.WriteFile(filepath.Join(dst, "empty"), []byte("stale"), 0o666)) // to be replaced
	must(extractDir(bytes.NewReader(buf.Bytes()), dst))

	data, err := os.ReadFile(filepath.Join(dst, "a/b/data"))
	must(err)
	if string(data) != "hello" {
		t.Errorf("unexpected contents %q", data)
	}

	for path, mode := range map[string]os.FileMode{
		".":        os.ModeDir | 0o770 | os.ModeSetgid | os.ModeSticky,
		"a":        os.ModeDir | 0o750,
		"a/b":      os.ModeDir | 0o500,
		"a/b/data": 0o640,
		"empty":    0o600,
		"sticky":   0o644 | os.ModeSetgid,
		"fifo":     os.ModeNamedPipe | 0o620,
	} {
		info, err := os.Lstat(filepath.Join(dst, path))
		must(err)
		if info.Mode() != mode {
			t.Errorf("%s: expected mode %v, got %v", path, mode, info.Mode())
		}
	}

	if info, _ := os.Stat(filepath.Join(dst, "empty")); info.Size() != 0 {
		t.Errorf("stale file was not replaced")
	}
	for _, path := range []string{"cedana-gpu.0123", "cedana-gpu.0123.misc"} {
		if _, err := os.Lstat(filepath.Join(dst, path)); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s: the GPU controller's, should not have been archived (%v)", path, err)
		}
	}
	if _, err := os.Lstat(filepath.Join(dst, "a/cedana-gpu.4567")); err != nil {
		t.Errorf("a/cedana-gpu.4567: the job's, should have been archived: %v", err)
	}

	for _, path := range []string{".", "a", "a/b/data"} {
		info, err := os.Stat(filepath.Join(dst, path))
		must(err)
		if !info.ModTime().Equal(mtime) {
			t.Errorf("%s: expected mtime %v, got %v", path, mtime, info.ModTime())
		}
	}

	for path, attr := range map[string]string{"sticky": "user.test", "a/b/data": "security.capability"} {
		want := make([]byte, 64)
		n, err := unix.Getxattr(filepath.Join(src, path), attr, want)
		if err != nil {
			continue // couldn't be set here
		}
		got := make([]byte, 64)
		m, err := unix.Getxattr(filepath.Join(dst, path), attr, got)
		if err != nil {
			t.Errorf("%s: expected xattr %s, got %v", path, attr, err)
		} else if !bytes.Equal(want[:n], got[:m]) {
			t.Errorf("%s: expected xattr %s %q, got %q", path, attr, want[:n], got[:m])
		}
	}

	for link, target := range map[string]string{"link": "a/b/data", "abs": "/etc/passwd"} {
		got, err := os.Readlink(filepath.Join(dst, link))
		must(err)
		if got != target {
			t.Errorf("%s: expected target %q, got %q", link, target, got)
		}
	}

	// Still the one file, by either name
	must(os.WriteFile(filepath.Join(dst, "hardlink"), []byte("changed"), 0o640))
	data, err = os.ReadFile(filepath.Join(dst, "a/b/data"))
	must(err)
	if string(data) != "changed" {
		t.Errorf("hardlink is a file of its own, other name still has %q", data)
	}
}

func TestArchiveDirLimit(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "small"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Takes up nothing, but would be written in full
	sparse, err := os.Create(filepath.Join(src, "sparse"))
	if err != nil {
		t.Fatal(err)
	}
	if err := sparse.Truncate(1 << 40); err != nil {
		t.Skipf("no sparse files here: %v", err)
	}
	sparse.Close()

	size, err := contentSize(src)
	if err != nil || size != 1<<40+5 {
		t.Errorf("expected a size of %d, got %d, %v", 1<<40+5, size, err)
	}

	var buf bytes.Buffer
	written, err := archiveDir(src, &buf, 1<<20)
	if err == nil {
		t.Error("expected an error")
	}
	if written > 5 || buf.Len() > 1<<20 {
		t.Errorf("wrote %d bytes of contents, %d in all", written, buf.Len())
	}

	if _, err := archiveDir(src, &bytes.Buffer{}, 4); err == nil {
		t.Error("expected an error with a limit below the smallest file")
	}
}

// Needs a mount to cross, so only runs for root
func TestExtractDirStaysOnFilesystem(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("needs root to mount")
	}

	src := t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, "cache"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "cache/data"), []byte("from the dump"), 0o644); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if _, err := archiveDir(src, &buf, noLimit); err != nil {
		t.Fatal(err)
	}

	dst := t.TempDir()
	mounted := filepath.Join(dst, "cache")
	if err := os.Mkdir(mounted, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mount("tmpfs", mounted, "tmpfs", 0, ""); err != nil {
		t.Skipf("not allowed to mount: %v", err)
	}
	defer syscall.Unmount(mounted, syscall.MNT_DETACH)

	before, err := os.Stat(mounted)
	if err != nil {
		t.Fatal(err)
	}

	if err := extractDir(bytes.NewReader(buf.Bytes()), dst); err == nil {
		t.Error("expected an error")
	}
	if entries, _ := os.ReadDir(mounted); len(entries) != 0 {
		t.Errorf("wrote into the other filesystem: %v", entries)
	}
	if after, _ := os.Stat(mounted); after.Mode() != before.Mode() {
		t.Errorf("changed the mode of the other filesystem from %v to %v", before.Mode(), after.Mode())
	}
}

func TestExtractDirStaysInside(t *testing.T) {
	outside := t.TempDir()

	archive := func(entries ...tar.Header) *bytes.Reader {
		var buf bytes.Buffer
		tw := tar.NewWriter(&buf)
		for _, header := range entries {
			header.Size = 0
			if err := tw.WriteHeader(&header); err != nil {
				t.Fatal(err)
			}
		}
		tw.Close()
		return bytes.NewReader(buf.Bytes())
	}

	t.Run("DotDot", func(t *testing.T) {
		err := extractDir(archive(tar.Header{Name: "../escaped", Typeflag: tar.TypeReg, Mode: 0o644}), t.TempDir())
		if err == nil {
			t.Error("expected an error")
		}
	})

	t.Run("Absolute", func(t *testing.T) {
		err := extractDir(archive(tar.Header{Name: filepath.Join(outside, "escaped"), Typeflag: tar.TypeReg, Mode: 0o644}), t.TempDir())
		if err == nil {
			t.Error("expected an error")
		}
	})

	t.Run("LinkToOutside", func(t *testing.T) {
		err := extractDir(archive(
			tar.Header{Name: "in", Typeflag: tar.TypeLink, Linkname: "../" + filepath.Base(outside) + "/escaped"},
		), t.TempDir())
		if err == nil {
			t.Error("expected an error")
		}
	})

	t.Run("RootNotADirectory", func(t *testing.T) {
		err := extractDir(archive(tar.Header{Name: ".", Typeflag: tar.TypeReg, Mode: 0o644}), t.TempDir())
		if err == nil {
			t.Error("expected an error")
		}
	})

	t.Run("ThroughExistingSymlink", func(t *testing.T) {
		dst := t.TempDir()
		if err := os.Symlink(outside, filepath.Join(dst, "out")); err != nil {
			t.Fatal(err)
		}
		err := extractDir(archive(
			tar.Header{Name: "out", Typeflag: tar.TypeDir, Mode: 0o777, Uid: os.Getuid(), Gid: os.Getgid()},
			tar.Header{Name: "out/escaped", Typeflag: tar.TypeReg, Mode: 0o644, Uid: os.Getuid(), Gid: os.Getgid()},
		), dst)
		if err == nil {
			t.Error("expected an error")
		}
	})

	t.Run("ThroughSymlink", func(t *testing.T) {
		err := extractDir(archive(
			tar.Header{Name: "out", Typeflag: tar.TypeSymlink, Linkname: outside, Uid: os.Getuid(), Gid: os.Getgid()},
			tar.Header{Name: "out/escaped", Typeflag: tar.TypeReg, Mode: 0o644, Uid: os.Getuid(), Gid: os.Getgid()},
		), t.TempDir())
		if err == nil {
			t.Error("expected an error")
		}
	})

	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Errorf("wrote outside of the directory: %v", entries)
	}
}

func TestSaveLoadPrivateMounts(t *testing.T) {
	fs := afero.NewMemMapFs()

	got, err := loadPrivateMounts(fs)
	if err != nil || got != nil {
		t.Fatalf("expected nothing from an empty dump, got %v, %v", got, err)
	}

	expected := []privateMount{{Mountpoint: "/var/tmp", FSType: TMPFS, Archive: "private_mount-0.tar"}}
	if err := savePrivateMounts(fs, expected); err != nil {
		t.Fatal(err)
	}
	got, err = loadPrivateMounts(fs)
	if err != nil || len(got) != 1 || got[0] != expected[0] {
		t.Fatalf("expected %v, got %v, %v", expected, got, err)
	}

	// Not being able to tell is not the same as there being none
	if _, err := loadPrivateMounts(failingFs{fs}); err == nil {
		t.Error("expected an error")
	}

	for _, bad := range []string{
		`[{"mountpoint":"/var/tmp","fstype":"tmpfs","archive":"../../etc/shadow"}]`,
		`[{"mountpoint":"/var/tmp","fstype":"tmpfs","archive":""}]`,
		`[{"mountpoint":"var/tmp","fstype":"tmpfs","archive":"a.tar"}]`,
	} {
		fs := afero.NewMemMapFs()
		afero.WriteFile(fs, PRIVATE_MOUNTS_FILE, []byte(bad), 0o644)
		if _, err := loadPrivateMounts(fs); err == nil {
			t.Errorf("expected an error for %s", bad)
		}
	}
}

type failingFs struct{ afero.Fs }

func (failingFs) Open(string) (afero.File, error) { return nil, errors.New("connection reset") }

func TestExtractLeavesWhatItMayNotSet(t *testing.T) {
	// The root of the filesystem is nobody's to chown or chmod but root's, as the root of a
	// job's mount is the launcher's. Everything is a no-op or left alone, never an error.
	root, err := os.OpenRoot("/")
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	info, err := root.Lstat(".")
	if err != nil {
		t.Fatal(err)
	}
	st := info.Sys().(*syscall.Stat_t)
	if err := setOwner(root, ".", int(st.Uid), int(st.Gid)); err != nil {
		t.Errorf("the owner it has already: %v", err)
	}
	if err := setMode(root, ".", info.Mode()); err != nil {
		t.Errorf("the mode it has already: %v", err)
	}
	if os.Getuid() != 0 {
		if err := setOwner(root, ".", os.Getuid(), os.Getgid()); err != nil {
			t.Errorf("an owner we may not set: %v", err)
		}
		if err := setMode(root, ".", info.Mode()^0o001); err != nil {
			t.Errorf("a mode we may not set: %v", err)
		}
		if err := setTimes(root, ".", time.Unix(0, 0), time.Unix(0, 0)); err != nil {
			t.Errorf("times we may not set: %v", err)
		}

		// Anything inside the mount has to be as it was, though
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "file"), nil, 0o600); err != nil {
			t.Fatal(err)
		}
		inside, err := os.OpenRoot(dir)
		if err != nil {
			t.Fatal(err)
		}
		defer inside.Close()
		if err := setOwner(inside, "file", 0, 0); err == nil {
			t.Error("expected giving a file to root to fail")
		}
	}
}

//go:build linux

package filesystem

import (
	"archive/tar"
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/afero"
)

func TestArchiveExtractDir(t *testing.T) {
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
	must(os.Chtimes(filepath.Join(src, "a/b/data"), mtime, mtime))
	must(os.Chtimes(filepath.Join(src, "a"), mtime, mtime))

	var buf bytes.Buffer
	must(archiveDir(src, &buf))

	dst := t.TempDir()
	must(os.WriteFile(filepath.Join(dst, "empty"), []byte("stale"), 0o666)) // to be replaced
	must(extractDir(bytes.NewReader(buf.Bytes()), dst))

	data, err := os.ReadFile(filepath.Join(dst, "a/b/data"))
	must(err)
	if string(data) != "hello" {
		t.Errorf("unexpected contents %q", data)
	}

	for path, mode := range map[string]os.FileMode{
		"a":        os.ModeDir | 0o750,
		"a/b/data": 0o640,
		"empty":    0o600,
		"sticky":   0o644 | os.ModeSetgid,
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

	for _, path := range []string{"a", "a/b/data"} {
		info, err := os.Stat(filepath.Join(dst, path))
		must(err)
		if !info.ModTime().Equal(mtime) {
			t.Errorf("%s: expected mtime %v, got %v", path, mtime, info.ModTime())
		}
	}

	for link, target := range map[string]string{"link": "a/b/data", "abs": "/etc/passwd"} {
		got, err := os.Readlink(filepath.Join(dst, link))
		must(err)
		if got != target {
			t.Errorf("%s: expected target %q, got %q", link, target, got)
		}
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

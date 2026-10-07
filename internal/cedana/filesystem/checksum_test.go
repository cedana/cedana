package filesystem

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cedana/cedana/pkg/config"
	cedana_io "github.com/cedana/cedana/pkg/io"
)

func sumOf(t *testing.T, data []byte) string {
	t.Helper()
	sum, err := cedana_io.ChecksumOf(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return sum
}

func TestCreateReturnsAChecksummingWriter(t *testing.T) {
	config.Global.Checkpoint.Checksum = true
	path := filepath.Join(t.TempDir(), "dump.tar.lz4")
	w, err := (&Storage{}).Create(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("compressed bytes")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if got := cedana_io.ChecksumOfWriter(w); got != sumOf(t, []byte("compressed bytes")) {
		t.Fatalf("writer checksum = %q", got)
	}
	stored, _ := os.ReadFile(path)
	if string(stored) != "compressed bytes" {
		t.Fatalf("file holds %q", stored)
	}

	config.Global.Checkpoint.Checksum = false
	w, _ = (&Storage{}).Create(context.Background(), filepath.Join(t.TempDir(), "off"))
	w.Close()
	if got := cedana_io.ChecksumOfWriter(w); got != "" {
		t.Fatalf("with the checksum off the writer must know none, got %q", got)
	}
}

func TestChecksumPathOfATarball(t *testing.T) {
	config.Global.Checkpoint.Checksum = true
	path := filepath.Join(t.TempDir(), "dump.tar.lz4")
	os.WriteFile(path, []byte("tarball"), 0o644)
	got, err := (&Storage{}).ChecksumPath(context.Background(), path)
	if err != nil || got != sumOf(t, []byte("tarball")) {
		t.Fatalf("ChecksumPath = %q, %v", got, err)
	}
}

func TestChecksumPathOfADirectoryIsItsManifest(t *testing.T) {
	config.Global.Checkpoint.Checksum = true
	dir := t.TempDir()
	for name, content := range map[string]string{"pages-1.img": "pages", "core-1.img": "core", "criu-dump.log": "x", "stats-dump": "x"} {
		os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644)
	}
	os.Mkdir(filepath.Join(dir, "sub"), 0o755)
	os.WriteFile(filepath.Join(dir, "sub", "ignored"), []byte("x"), 0o644)
	// A link to a file is hashed as that file; a link to a directory is left out
	os.Symlink("core-1.img", filepath.Join(dir, "link"))
	os.Symlink("sub", filepath.Join(dir, "link-to-dir"))

	got, err := (&Storage{}).ChecksumPath(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	want := cedana_io.ManifestChecksum([]cedana_io.ManifestEntry{
		{Name: "core-1.img", Checksum: sumOf(t, []byte("core"))},
		{Name: "link", Checksum: sumOf(t, []byte("core"))},
		{Name: "pages-1.img", Checksum: sumOf(t, []byte("pages"))},
	})
	if got != want {
		t.Fatalf("ChecksumPath = %s, want %s", got, want)
	}

	empty := t.TempDir()
	os.WriteFile(filepath.Join(empty, "criu-dump.log"), []byte("x"), 0o644)
	if got, err := (&Storage{}).ChecksumPath(context.Background(), empty); err != nil || got != "" {
		t.Fatalf("a directory with nothing to hash must have no checksum, got %q, %v", got, err)
	}
}

func TestChecksumPathOfAMissingPath(t *testing.T) {
	config.Global.Checkpoint.Checksum = true
	if _, err := (&Storage{}).ChecksumPath(context.Background(), filepath.Join(t.TempDir(), "gone")); err == nil {
		t.Fatal("expected an error")
	}
}

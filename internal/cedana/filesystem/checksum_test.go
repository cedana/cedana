package filesystem

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
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

// A directory's checksum is that of the manifest the mem-verify plugin wrote in it
// during the dump; nothing else is read
func TestChecksumPathOfADirectoryIsItsManifest(t *testing.T) {
	config.Global.Checkpoint.Checksum = true
	dir := t.TempDir()
	for name, content := range map[string]string{"pages-1.img": "pages", "core-1.img": "core", "criu-dump.log": "x", "stats-dump": "x", "mem-verify-gpu-0": "gpu-mem-w0-0 crc32c:deadbeef\n"} {
		os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644)
	}
	os.Mkdir(filepath.Join(dir, "sub"), 0o755)
	os.WriteFile(filepath.Join(dir, "sub", "ignored"), []byte("x"), 0o644)
	entries := []cedana_io.ManifestEntry{
		{Name: "core-1.img", Checksum: sumOf(t, []byte("core"))},
		{Name: "gpu-mem-w0-0", Checksum: "crc32c:deadbeef"},
		{Name: "pages-1.img", Checksum: sumOf(t, []byte("pages"))},
	}
	manifest := manifestText(entries)
	os.WriteFile(filepath.Join(dir, cedana_io.ManifestImage), []byte(manifest), 0o644)

	got, err := (&Storage{}).ChecksumPath(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if want := cedana_io.ManifestChecksum(entries); got != want {
		t.Fatalf("ChecksumPath = %s, want %s", got, want)
	}
	// The value is the CRC32C of the manifest's bytes, the one the plugin notifies
	if got != sumOf(t, []byte(manifest)) {
		t.Fatalf("ChecksumPath = %s, want the manifest's own checksum %s", got, sumOf(t, []byte(manifest)))
	}

	// Computed from the files, the manifest is the one the plugin wrote, without the
	// GPU controller's own lines, which the plugin merges
	computed, err := manifestOfDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}
	if want := entries[:1]; !slices.Equal(computed, append(want, entries[2])) {
		t.Fatalf("manifest of the files = %v", computed)
	}

	// Without a manifest there is no checksum, and no error: the plugin is missing
	os.Remove(filepath.Join(dir, cedana_io.ManifestImage))
	if got, err := (&Storage{}).ChecksumPath(context.Background(), dir); err != nil || got != "" {
		t.Fatalf("a directory without a manifest must have no checksum, got %q, %v", got, err)
	}

	// A manifest that is not one is an error
	os.WriteFile(filepath.Join(dir, cedana_io.ManifestImage), []byte("pages-1.img nohash\n"), 0o644)
	if _, err := (&Storage{}).ChecksumPath(context.Background(), dir); err == nil {
		t.Fatal("a bad manifest line must be an error")
	}
}

// manifestText is the manifest as the plugin writes it: the text ManifestChecksum hashes
func manifestText(entries []cedana_io.ManifestEntry) string {
	var b bytes.Buffer
	for _, e := range entries {
		b.WriteString(e.Name + " " + e.Checksum + "\n")
	}
	return b.String()
}

func TestChecksumPathOfAMissingPath(t *testing.T) {
	config.Global.Checkpoint.Checksum = true
	if _, err := (&Storage{}).ChecksumPath(context.Background(), filepath.Join(t.TempDir(), "gone")); err == nil {
		t.Fatal("expected an error")
	}
}

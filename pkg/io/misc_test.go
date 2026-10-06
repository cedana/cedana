package io

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestListTarAndOpenTarEntry(t *testing.T) {
	for _, compression := range []string{"tar", "gzip", "lz4", "zlib"} {
		t.Run(compression, func(t *testing.T) {
			src := t.TempDir()
			small := []byte("hello")
			large := bytes.Repeat([]byte("x"), 4096)
			if err := os.WriteFile(filepath.Join(src, "a.img"), small, 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(src, "b.img"), large, 0o644); err != nil {
				t.Fatal(err)
			}

			var tarball bytes.Buffer
			if err := Tar(src, &tarball, compression, false); err != nil {
				t.Fatal(err)
			}

			entries, err := ListTar(bytes.NewReader(tarball.Bytes()), compression)
			if err != nil {
				t.Fatal(err)
			}
			sizes := map[string]int64{}
			for _, entry := range entries {
				if !entry.IsDir {
					sizes[entry.Name] = entry.Size
				}
			}
			if len(sizes) != 2 || sizes["a.img"] != int64(len(small)) || sizes["b.img"] != int64(len(large)) {
				t.Fatalf("unexpected entries: %+v", entries)
			}

			reader, err := OpenTarEntry(bytes.NewReader(tarball.Bytes()), compression, "b.img")
			if err != nil {
				t.Fatal(err)
			}
			data, err := io.ReadAll(reader)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(data, large) {
				t.Fatalf("entry contents mismatch: got %d bytes", len(data))
			}

			if _, err := OpenTarEntry(bytes.NewReader(tarball.Bytes()), compression, "missing.img"); err == nil {
				t.Fatal("expected error for missing entry")
			}
		})
	}
}

func TestTarCompressionFromPath(t *testing.T) {
	cases := []struct {
		path        string
		compression string
		ok          bool
	}{
		{"csx://1234.tar.lz4", "lz4", true},
		{"s3://bucket/dir/ckpt.tar.gz", "gzip", true},
		{"/tmp/ckpt.tar.zlib", "zlib", true},
		{"/tmp/ckpt.tar", "tar", true},
		{"/tmp/ckpt", "", false},
		{"s3://bucket/dir/pages-1.img", "", false},
		{"/tmp/ckpt.tar.bogus", "", false},
	}
	for _, c := range cases {
		compression, ok := TarCompressionFromPath(c.path)
		if compression != c.compression || ok != c.ok {
			t.Errorf("%s: got (%q, %v), want (%q, %v)", c.path, compression, ok, c.compression, c.ok)
		}
	}
}

var errDestination = errors.New("destination failed")

// Accepts limit bytes in total, then fails.
type failingWriter struct {
	limit   int
	written int
}

func (f *failingWriter) Write(p []byte) (int, error) {
	if f.written+len(p) > f.limit {
		return 0, errDestination
	}
	f.written += len(p)
	return len(p), nil
}

var checkpointData = bytes.Repeat([]byte("checkpoint"), 100)

// Returns a destination that fails on the last bytes of what write sends it. Every
// compression writer ends its stream when it is closed, so that is where it fails.
func failingOnClose(t *testing.T, write func(dst io.Writer) error) *failingWriter {
	t.Helper()
	var whole bytes.Buffer
	if err := write(&whole); err != nil {
		t.Fatalf("write to a destination that does not fail: %v", err)
	}
	return &failingWriter{limit: whole.Len() - 1}
}

func TestWriteToReturnsCloseError(t *testing.T) {
	for _, compression := range []string{"lz4", "gzip", "zlib"} {
		t.Run(compression, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "src")
			if err := os.WriteFile(path, checkpointData, 0o644); err != nil {
				t.Fatal(err)
			}
			src, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer src.Close()

			dst := failingOnClose(t, func(dst io.Writer) error {
				_, err := WriteTo(src, dst, compression)
				return err
			})
			if _, err := src.Seek(0, io.SeekStart); err != nil {
				t.Fatal(err)
			}

			_, err = WriteTo(src, dst, compression)
			if !errors.Is(err, errDestination) {
				t.Fatalf("expected the error from closing the compression writer, got %v", err)
			}
		})
	}
}

func TestWriteToWritesCompressed(t *testing.T) {
	for _, compression := range []string{"none", "lz4", "gzip", "zlib"} {
		t.Run(compression, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "src")
			if err := os.WriteFile(path, checkpointData, 0o644); err != nil {
				t.Fatal(err)
			}
			src, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer src.Close()

			var dst bytes.Buffer
			n, err := WriteTo(src, &dst, compression)
			if err != nil {
				t.Fatalf("write failed: %v", err)
			}
			if n != int64(len(checkpointData)) {
				t.Fatalf("wrote %d bytes, expected %d", n, len(checkpointData))
			}

			reader, err := NewCompressionReader(&dst, compression)
			if err != nil {
				t.Fatal(err)
			}
			defer reader.Close()
			var got bytes.Buffer
			if _, err := got.ReadFrom(reader); err != nil {
				t.Fatalf("read back failed: %v", err)
			}
			if !bytes.Equal(got.Bytes(), checkpointData) {
				t.Fatal("what was read back differs from what was written")
			}
		})
	}
}

func TestTarReturnsCloseError(t *testing.T) {
	for _, compression := range []string{"lz4", "gzip", "zlib"} {
		t.Run(compression, func(t *testing.T) {
			src := t.TempDir()
			if err := os.WriteFile(filepath.Join(src, "pages-1.img"), checkpointData, 0o644); err != nil {
				t.Fatal(err)
			}

			dst := failingOnClose(t, func(dst io.Writer) error {
				return Tar(src, dst, compression, false)
			})

			err := Tar(src, dst, compression, false)
			if !errors.Is(err, errDestination) {
				t.Fatalf("expected the error from closing the compression writer, got %v", err)
			}
		})
	}
}

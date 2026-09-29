package io

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

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

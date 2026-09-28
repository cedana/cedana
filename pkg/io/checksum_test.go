package io

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
)

type bufferCloser struct {
	bytes.Buffer
	closed bool
}

func (b *bufferCloser) Close() error {
	b.closed = true
	return nil
}

// Accepts at most limit bytes per write, then fails.
type shortWriter struct {
	bufferCloser
	limit int
}

func (s *shortWriter) Write(p []byte) (int, error) {
	if len(p) > s.limit {
		n, _ := s.bufferCloser.Write(p[:s.limit])
		return n, errors.New("short write")
	}
	return s.bufferCloser.Write(p)
}

func sha256Of(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func TestChecksumWriter(t *testing.T) {
	t.Run("MatchesDirectHash", func(t *testing.T) {
		dst := &bufferCloser{}
		w := NewChecksumWriter(dst)

		chunks := [][]byte{[]byte("hello "), []byte("checkpoint"), {}, []byte{0, 1, 2, 255}}
		var all []byte
		for _, chunk := range chunks {
			n, err := w.Write(chunk)
			if err != nil {
				t.Fatalf("write failed: %v", err)
			}
			if n != len(chunk) {
				t.Fatalf("wrote %d bytes, expected %d", n, len(chunk))
			}
			all = append(all, chunk...)
		}

		if !bytes.Equal(dst.Bytes(), all) {
			t.Fatalf("underlying writer received %q, expected %q", dst.Bytes(), all)
		}
		if got, want := w.Sum(), sha256Of(all); got != want {
			t.Fatalf("checksum is %s, expected %s", got, want)
		}
	})

	t.Run("Empty", func(t *testing.T) {
		w := NewChecksumWriter(&bufferCloser{})
		if got, want := w.Sum(), sha256Of(nil); got != want {
			t.Fatalf("checksum is %s, expected %s", got, want)
		}
	})

	t.Run("ShortWriteHashesOnlyWrittenBytes", func(t *testing.T) {
		dst := &shortWriter{limit: 4}
		w := NewChecksumWriter(dst)

		n, err := w.Write([]byte("checkpoint"))
		if err == nil {
			t.Fatal("expected the short write to fail")
		}
		if n != 4 {
			t.Fatalf("wrote %d bytes, expected 4", n)
		}
		if got, want := w.Sum(), sha256Of(dst.Bytes()); got != want {
			t.Fatalf("checksum is %s, expected %s of the bytes that reached the writer", got, want)
		}
	})

	t.Run("ClosesUnderlyingWriter", func(t *testing.T) {
		dst := &bufferCloser{}
		w := NewChecksumWriter(dst)
		if err := w.Close(); err != nil {
			t.Fatalf("close failed: %v", err)
		}
		if !dst.closed {
			t.Fatal("underlying writer was not closed")
		}
	})
}

func TestManifest(t *testing.T) {
	t.Run("FixedEntriesGiveFixedChecksum", func(t *testing.T) {
		m := &Manifest{}
		m.Add("img-0.lz4", "sha256:aa")
		m.Add("img-1.lz4", "sha256:bb")

		want := sha256Of([]byte("img-0.lz4 sha256:aa\nimg-1.lz4 sha256:bb\n"))
		if got := m.Sum(); got != want {
			t.Fatalf("checksum is %s, expected %s", got, want)
		}
		if got := m.Sum(); got != want {
			t.Fatalf("checksum changed on second call: %s, expected %s", got, want)
		}
	})

	t.Run("OrderMatters", func(t *testing.T) {
		a := &Manifest{}
		a.Add("img-0", "sha256:aa")
		a.Add("img-1", "sha256:bb")

		b := &Manifest{}
		b.Add("img-1", "sha256:bb")
		b.Add("img-0", "sha256:aa")

		if a.Sum() == b.Sum() {
			t.Fatal("manifests with entries in different order have the same checksum")
		}
	})

	t.Run("NoEntries", func(t *testing.T) {
		m := &Manifest{}
		if got := m.Sum(); got != "" {
			t.Fatalf("checksum is %q, expected an empty string", got)
		}
	})
}

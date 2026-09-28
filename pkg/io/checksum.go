package io

import (
	"crypto/sha256"
	"encoding/hex"
	"hash"
	"io"
)

const CHECKSUM_ALGORITHM = "sha256"

// ChecksumWriter hashes everything written through it to the underlying writer.
type ChecksumWriter struct {
	wc io.WriteCloser
	h  hash.Hash
}

func NewChecksumWriter(wc io.WriteCloser) *ChecksumWriter {
	return &ChecksumWriter{wc: wc, h: sha256.New()}
}

func (w *ChecksumWriter) Write(p []byte) (n int, err error) {
	n, err = w.wc.Write(p)
	w.h.Write(p[:n])
	return n, err
}

func (w *ChecksumWriter) Close() error {
	return w.wc.Close()
}

// Sum returns the checksum of everything written so far, as "<algorithm>:<hex>".
// It is final only once the writers stacked on top of this one are closed.
func (w *ChecksumWriter) Sum() string {
	return formatChecksum(w.h)
}

type manifestEntry struct {
	name string
	sum  string
}

// Manifest combines the checksums of several objects into one.
// Entries are hashed in the order they were added.
type Manifest struct {
	entries []manifestEntry
}

func (m *Manifest) Add(name, sum string) {
	m.entries = append(m.entries, manifestEntry{name: name, sum: sum})
}

// Sum returns the checksum of the manifest, as "<algorithm>:<hex>". The manifest
// is one line per entry, "<name> <sum>\n". Returns "" if there are no entries.
func (m *Manifest) Sum() string {
	if len(m.entries) == 0 {
		return ""
	}
	h := sha256.New()
	for _, entry := range m.entries {
		io.WriteString(h, entry.name+" "+entry.sum+"\n")
	}
	return formatChecksum(h)
}

func formatChecksum(h hash.Hash) string {
	return CHECKSUM_ALGORITHM + ":" + hex.EncodeToString(h.Sum(nil))
}

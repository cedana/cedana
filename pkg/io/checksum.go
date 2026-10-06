package io

// Integrity checksums of checkpoints as stored. The value is a string that names
// its algorithm, "crc32c:<lowercase hex>", so a later algorithm changes only the
// prefix. CRC32C (Castagnoli) detects corruption, runs in hardware on SSE4.2 and
// ARMv8, and is faster than the compression it follows, so hashing a stream as it
// is written costs no time.

import (
	"fmt"
	"hash"
	"hash/crc32"
	"io"
	"strings"
)

const CHECKSUM_ALGORITHM = "crc32c"

var castagnoli = crc32.MakeTable(crc32.Castagnoli)

// ChecksumWriter hashes everything written through it to the underlying writer,
// so the checksum is of the bytes as stored.
type ChecksumWriter struct {
	w io.Writer
	h hash.Hash32
}

func NewChecksumWriter(w io.Writer) *ChecksumWriter {
	return &ChecksumWriter{w: w, h: crc32.New(castagnoli)}
}

func (c *ChecksumWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.h.Write(p[:n])
	return n, err
}

// Close closes the underlying writer if it can be closed
func (c *ChecksumWriter) Close() error {
	if closer, ok := c.w.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

// Sum returns the checksum of everything written so far
func (c *ChecksumWriter) Sum() string {
	return FormatChecksum(c.h.Sum32())
}

func FormatChecksum(sum uint32) string {
	return fmt.Sprintf("%s:%08x", CHECKSUM_ALGORITHM, sum)
}

// ChecksumOf reads r to its end and returns the checksum of what it read
func ChecksumOf(r io.Reader) (string, error) {
	h := crc32.New(castagnoli)
	if _, err := io.Copy(h, r); err != nil {
		return "", err
	}
	return FormatChecksum(h.Sum32()), nil
}

// A ManifestEntry is one file of a checkpoint made of several files: a shard of a
// streamed checkpoint, or a file of an uncompressed directory.
type ManifestEntry struct {
	Name     string
	Checksum string
}

// ManifestChecksum returns the checksum of a checkpoint made of several files: that
// of a manifest with one line per entry, "<name> <checksum>\n", in the order given.
// Shards are given in shard order; the files of a directory in order of name.
// Returns "" for no entries.
func ManifestChecksum(entries []ManifestEntry) string {
	if len(entries) == 0 {
		return ""
	}
	var manifest strings.Builder
	for _, entry := range entries {
		fmt.Fprintf(&manifest, "%s %s\n", entry.Name, entry.Checksum)
	}
	return FormatChecksum(crc32.Checksum([]byte(manifest.String()), castagnoli))
}

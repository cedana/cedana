package io

// Integrity checksums of checkpoints as stored. The value is a string that names
// its algorithm, "crc32c:<lowercase hex>", so a later algorithm changes only the
// prefix. CRC32C (Castagnoli) detects corruption, runs in hardware on SSE4.2 and
// ARMv8, and is faster than the compression it follows, so hashing a stream as it
// is written costs no time.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"hash"
	"hash/crc32"
	"io"
	"strings"

	"github.com/rs/zerolog/log"
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

// Checksum implements Checksummer
func (c *ChecksumWriter) Checksum() string {
	return c.Sum()
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

// Checksummer is implemented by a writer that knows the checksum of what was
// written through it, "<algorithm>:<hex>" of the bytes as stored. The value is
// valid after Close; "" if none was computed. A storage's Create may return one.
type Checksummer interface {
	Checksum() string
}

// PathChecksummer is implemented by a storage that can compute the checksum of a
// path it holds: a tarball's bytes, or the manifest of a directory. Returns "" for
// a directory with nothing to hash.
type PathChecksummer interface {
	ChecksumPath(ctx context.Context, path string) (string, error)
}

// ChecksumOfWriter returns the checksum a writer knows, or "" if it knows none
func ChecksumOfWriter(w io.Writer) string {
	if c, ok := w.(Checksummer); ok {
		return c.Checksum()
	}
	return ""
}

// ManifestExcluded are the files of a directory checkpoint that a restore rewrites,
// and the manifest's own files; they are left out of its manifest
var ManifestExcluded = []string{"criu-dump.log", "criu-restore.log", "stats-dump", "stats-restore", ManifestImage, ManifestResult}

// ManifestImage is the image the mem-verify CRIU plugin writes during a dump: the
// manifest of the checkpoint's files, one line per file in the text ManifestChecksum
// hashes, so that the CRC32C of the image is the checkpoint's checksum
const ManifestImage = "mem-verify-manifest"

// ManifestResult is the file the plugin writes its restore result to when CRIU does
// not run under RPC, as in the plugin's tests
const ManifestResult = "mem-verify-result"

// GPULinesPrefix names the images the GPU controller writes with the checksum of its
// own files, one per worker, which the plugin merges into the manifest
const GPULinesPrefix = "mem-verify-gpu-"

// ParseManifest reads a manifest, one line "<name> <checksum>" per file
func ParseManifest(r io.Reader) ([]ManifestEntry, error) {
	var entries []ManifestEntry
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		name, sum, ok := strings.Cut(line, " ")
		if !ok || name == "" || !strings.Contains(sum, ":") {
			return nil, fmt.Errorf("bad manifest line %q", line)
		}
		entries = append(entries, ManifestEntry{Name: name, Checksum: sum})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}

// A ManifestNotice is what the mem-verify plugin sends on the notification
// "mem-verify-manifest" when it has written the manifest: the manifest's checksum,
// which is the checkpoint's, and its line count
type ManifestNotice struct {
	Manifest string `json:"manifest"`
	Files    int    `json:"files"`
}

func ParseManifestNotice(payload []byte) (ManifestNotice, error) {
	var notice ManifestNotice
	if err := json.Unmarshal(payload, &notice); err != nil {
		return notice, fmt.Errorf("bad manifest notice %q: %w", payload, err)
	}
	if notice.Manifest == "" {
		return notice, fmt.Errorf("manifest notice without a checksum: %q", payload)
	}
	return notice, nil
}

// RecordedChecksum is the checksum a dump records for a checkpoint: the manifest's,
// which the mem-verify plugin sent from CRIU, whatever the storage wrote. The value
// the storage's writer computed over what it wrote, a tarball or the shards, is logged
// beside it as a check of the transfer. "" with a warning when no manifest came: the
// plugin is not installed, or CRIU predates it.
func RecordedChecksum(written, manifest, what string) string {
	if manifest == "" {
		log.Warn().Str("written", written).Msgf("no manifest checksum from CRIU for the %s: is the mem-verify plugin installed? no checksum recorded", what)
		return ""
	}
	log.Info().Str("written", written).Str("checksum", manifest).Msgf("the %s is recorded with the manifest's checksum", what)
	return manifest
}

package filesystem

// The local storage computes every checksum of what it holds: a writer returned by
// Create hashes the bytes written through it (cedana_io.Checksummer), and
// ChecksumPath reads a tarball or a directory on the node (cedana_io.PathChecksummer).

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"

	"github.com/cedana/cedana/pkg/config"
	cedana_io "github.com/cedana/cedana/pkg/io"
)

// checksumFile is a file that knows the checksum of what was written to it
type checksumFile struct {
	*cedana_io.ChecksumWriter
	file *os.File
}

func (f *checksumFile) Close() error {
	return f.file.Close()
}

// newWriter wraps a created file in a checksum writer when the checksum is on
func newWriter(file *os.File) io.WriteCloser {
	if !config.Global.Checkpoint.Checksum {
		return file
	}
	return &checksumFile{ChecksumWriter: cedana_io.NewChecksumWriter(file), file: file}
}

// ChecksumPath returns the checksum of a checkpoint at path: of the bytes of a
// tarball, or of the manifest of a directory, one line "<name> crc32c:<hex>" per
// regular file directly in it, in order of name, without the files a restore
// rewrites and without sub-directories. Returns "" when the checksum is off, or
// for a directory with nothing to hash.
func (s *Storage) ChecksumPath(_ context.Context, path string) (string, error) {
	if !config.Global.Checkpoint.Checksum {
		return "", nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("failed to stat %s: %w", path, err)
	}
	if !info.IsDir() {
		return checksumOfFile(path)
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return "", fmt.Errorf("failed to read %s: %w", path, err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if slices.Contains(cedana_io.ManifestExcluded, entry.Name()) {
			continue
		}
		// A link counts as what it points to
		info, err := os.Stat(filepath.Join(path, entry.Name()))
		if err != nil {
			return "", fmt.Errorf("failed to stat %s: %w", entry.Name(), err)
		}
		if !info.Mode().IsRegular() {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)

	manifest := make([]cedana_io.ManifestEntry, 0, len(names))
	for _, name := range names {
		sum, err := checksumOfFile(filepath.Join(path, name))
		if err != nil {
			return "", err
		}
		manifest = append(manifest, cedana_io.ManifestEntry{Name: name, Checksum: sum})
	}
	return cedana_io.ManifestChecksum(manifest), nil
}

func checksumOfFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("failed to open %s: %w", path, err)
	}
	defer file.Close()
	sum, err := cedana_io.ChecksumOf(file)
	if err != nil {
		return "", fmt.Errorf("failed to read %s: %w", path, err)
	}
	return sum, nil
}

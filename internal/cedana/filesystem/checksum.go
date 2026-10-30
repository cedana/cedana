package filesystem

// The local storage computes every checksum of what it holds: a writer returned by
// Create hashes the bytes written through it (cedana_io.Checksummer), and
// ChecksumPath reads a tarball, or the manifest of a directory, on the node
// (cedana_io.PathChecksummer).

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/cedana/cedana/pkg/config"
	cedana_io "github.com/cedana/cedana/pkg/io"
	"github.com/rs/zerolog/log"
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
// tarball, or of the manifest of a directory, which the mem-verify CRIU plugin
// wrote during the dump: one line "<name> crc32c:<hex>" per file, in order of
// name. Returns "" when the checksum is off, or for a directory without a
// manifest, with a warning: the plugin is not installed, or the checkpoint
// predates it. Nothing but the manifest is read.
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

	manifest, err := os.Open(filepath.Join(path, cedana_io.ManifestImage))
	if errors.Is(err, fs.ErrNotExist) {
		log.Warn().Str("path", path).Msg("no manifest in the checkpoint directory: is the mem-verify CRIU plugin installed? no checksum")
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("failed to open the manifest of %s: %w", path, err)
	}
	defer manifest.Close()
	entries, err := cedana_io.ParseManifest(manifest)
	if err != nil {
		return "", fmt.Errorf("bad manifest in %s: %w", path, err)
	}
	return cedana_io.ManifestChecksum(entries), nil
}

// manifestOfDirectory computes the manifest the plugin writes, from the files: every
// regular file directly in the directory, in order of name, without the excluded
// names, the plugin's own files and sub-directories. A test's check of a manifest.
func manifestOfDirectory(path string) ([]cedana_io.ManifestEntry, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", path, err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if slices.Contains(cedana_io.ManifestExcluded, entry.Name()) || strings.HasPrefix(entry.Name(), cedana_io.GPULinesPrefix) {
			continue
		}
		// A link counts as what it points to
		info, err := os.Stat(filepath.Join(path, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("failed to stat %s: %w", entry.Name(), err)
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
			return nil, err
		}
		manifest = append(manifest, cedana_io.ManifestEntry{Name: name, Checksum: sum})
	}
	return manifest, nil
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

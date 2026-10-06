// Package checksum computes the integrity checksum of a checkpoint on the node by
// reading it back through the daemon after the dump has returned. The containers
// run again at that time, so nothing is added to the dump.
//
// The value is the daemon's format, "crc32c:<hex>" of the bytes as stored:
//   - a tarball: the checksum of the tarball;
//   - an uncompressed directory: the checksum of a manifest with one line per
//     regular file directly in it, "<name> crc32c:<hex>\n", in order of name,
//     leaving out the files a restore rewrites and any sub-directory.
package checksum

import (
	"context"
	"fmt"
	"io"
	"slices"
	"sort"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	cedana_io "github.com/cedana/cedana/pkg/io"
	"google.golang.org/grpc"
)

// Excluded are the files of a directory checkpoint that a restore rewrites
var Excluded = []string{"criu-dump.log", "criu-restore.log", "stats-dump", "stats-restore"}

// Client is the part of the daemon client the checksum needs
type Client interface {
	ListPath(ctx context.Context, args *daemon.ListPathReq, opts ...grpc.CallOption) (*daemon.ListPathResp, error)
	ReadPath(ctx context.Context, args *daemon.ReadPathReq, opts ...grpc.CallOption) (io.ReadCloser, error)
}

// Path returns the checksum of the checkpoint at path, a tarball or an
// uncompressed directory, read through the daemon. Returns "" for a directory
// with no file to hash.
func Path(ctx context.Context, client Client, path string) (string, error) {
	if _, isTar := cedana_io.TarCompressionFromPath(path); isTar {
		return entry(ctx, client, path, "")
	}

	listed, err := client.ListPath(ctx, &daemon.ListPathReq{Path: path})
	if err != nil {
		return "", fmt.Errorf("listing %s: %w", path, err)
	}
	names := make([]string, 0, len(listed.GetEntries()))
	for _, e := range listed.GetEntries() {
		if e.GetIsDir() || slices.Contains(Excluded, e.GetName()) {
			continue
		}
		names = append(names, e.GetName())
	}
	sort.Strings(names)

	entries := make([]cedana_io.ManifestEntry, 0, len(names))
	for _, name := range names {
		sum, err := entry(ctx, client, path, name)
		if err != nil {
			return "", err
		}
		entries = append(entries, cedana_io.ManifestEntry{Name: name, Checksum: sum})
	}
	return cedana_io.ManifestChecksum(entries), nil
}

// entry is the checksum of path itself, or of one file in it
func entry(ctx context.Context, client Client, path, name string) (string, error) {
	reader, err := client.ReadPath(ctx, &daemon.ReadPathReq{Path: path, Entry: name})
	if err != nil {
		return "", fmt.Errorf("reading %s %s: %w", path, name, err)
	}
	defer reader.Close()
	sum, err := cedana_io.ChecksumOf(reader)
	if err != nil {
		return "", fmt.Errorf("reading %s %s: %w", path, name, err)
	}
	return sum, nil
}

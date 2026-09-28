package filesystem

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	criu_client "github.com/cedana/cedana/pkg/criu"
	"github.com/cedana/cedana/pkg/types"
	"google.golang.org/protobuf/proto"
)

// Local storage that reports itself as remote
type remoteStorage struct {
	Storage
}

func (s *remoteStorage) IsRemote() bool {
	return true
}

// Stands in for the CRIU dump. Writes images to the images directory,
// and triggers the post-dump hook as CRIU would.
func dumpImages(t *testing.T) types.Dump {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (func() <-chan int, error) {
		imagesDirectory := req.GetCriu().GetImagesDir()
		for i := range 3 {
			image := filepath.Join(imagesDirectory, fmt.Sprintf("pages-%d.img", i))
			if err := os.WriteFile(image, []byte(fmt.Sprintf("pages of image %d", i)), 0o644); err != nil {
				t.Fatalf("failed to write image: %v", err)
			}
		}
		return nil, opts.CRIUCallback.PostDump(ctx, req.GetCriu())
	}
}

func checksumOfFile(t *testing.T, path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read %s: %v", path, err)
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func TestDumpFilesystemChecksum(t *testing.T) {
	ctx := context.Background()

	for _, compression := range []string{"tar", "gzip", "lz4", "zlib"} {
		for _, leaveRunning := range []bool{false, true} {
			t.Run(fmt.Sprintf("Bundled/%s/LeaveRunning=%t", compression, leaveRunning), func(t *testing.T) {
				opts := types.Opts{
					WG:           &sync.WaitGroup{},
					CRIUCallback: &criu_client.NotifyCallbackMulti{},
					Storage:      &Storage{},
				}
				req := &daemon.DumpReq{
					Dir:         t.TempDir(),
					Name:        "dump",
					Compression: compression,
					Criu:        &criu_proto.CriuOpts{LeaveRunning: proto.Bool(leaveRunning)},
				}
				resp := &daemon.DumpResp{}

				_, err := DumpFilesystem(dumpImages(t))(ctx, opts, resp, req)
				if err != nil {
					t.Fatalf("dump failed: %v", err)
				}

				if len(resp.Paths) != 1 {
					t.Fatalf("expected 1 path, got %v", resp.Paths)
				}
				if len(resp.Checksums) != 1 {
					t.Fatalf("expected 1 checksum, got %v", resp.Checksums)
				}
				for path, checksum := range resp.Checksums {
					if !slices.Contains(resp.Paths, path) {
						t.Fatalf("checksum is for %s, which is not in paths %v", path, resp.Paths)
					}
					if expected := checksumOfFile(t, path); checksum != expected {
						t.Fatalf("checksum is %s, expected %s", checksum, expected)
					}
				}
			})
		}
	}

	t.Run("Uncompressed", func(t *testing.T) {
		opts := types.Opts{
			WG:           &sync.WaitGroup{},
			CRIUCallback: &criu_client.NotifyCallbackMulti{},
			Storage:      &Storage{},
		}
		req := &daemon.DumpReq{
			Dir:         t.TempDir(),
			Name:        "dump",
			Compression: "none",
		}
		resp := &daemon.DumpResp{}

		_, err := DumpFilesystem(dumpImages(t))(ctx, opts, resp, req)
		if err != nil {
			t.Fatalf("dump failed: %v", err)
		}

		if len(resp.Paths) != 1 {
			t.Fatalf("expected 1 path, got %v", resp.Paths)
		}
		if len(resp.Checksums) != 0 {
			t.Fatalf("expected no checksum, got %v", resp.Checksums)
		}
	})

	t.Run("Async", func(t *testing.T) {
		opts := types.Opts{
			WG:           &sync.WaitGroup{},
			CRIUCallback: &criu_client.NotifyCallbackMulti{},
			Storage:      &remoteStorage{},
		}
		req := &daemon.DumpReq{
			Dir:         t.TempDir(),
			Name:        fmt.Sprintf("dump-async-%d", os.Getpid()),
			Compression: "lz4",
			Async:       true,
		}
		resp := &daemon.DumpResp{}

		_, err := DumpFilesystem(dumpImages(t))(ctx, opts, resp, req)
		if err != nil {
			t.Fatalf("dump failed: %v", err)
		}

		// The response is what the caller receives, before the upload completes
		if len(resp.Paths) != 1 {
			t.Fatalf("expected 1 path, got %v", resp.Paths)
		}
		if len(resp.Checksums) != 0 {
			t.Fatalf("expected no checksum, got %v", resp.Checksums)
		}

		opts.WG.Wait()

		if _, err := os.Stat(resp.Paths[0]); err != nil {
			t.Fatalf("expected dump to be uploaded: %v", err)
		}
		if len(resp.Checksums) != 0 {
			t.Fatalf("expected no checksum after upload, got %v", resp.Checksums)
		}
	})
}

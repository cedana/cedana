package filesystem

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/pkg/config"
	criu_client "github.com/cedana/cedana/pkg/criu"
	cedana_io "github.com/cedana/cedana/pkg/io"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/pkg/upload"
	"google.golang.org/protobuf/proto"
)

// Local storage that reports itself as remote
type remoteStorage struct {
	Storage
}

func (s *remoteStorage) IsRemote() bool {
	return true
}

// Remote storage that cannot be written to
type failingStorage struct {
	remoteStorage
}

func (s *failingStorage) Create(_ context.Context, path string) (io.WriteCloser, error) {
	return nil, errors.New("storage unavailable")
}

func (s *failingStorage) Delete(_ context.Context, path string) error {
	return nil
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

func TestDumpFilesystem(t *testing.T) {
	config.Global.Checkpoint.Checksum = true
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
				if _, err := os.Stat(resp.Paths[0]); err != nil {
					t.Fatalf("tarball was not written: %v", err)
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
	})

	t.Run("Async", func(t *testing.T) {
		opts := types.Opts{
			WG:           &sync.WaitGroup{},
			CRIUCallback: &criu_client.NotifyCallbackMulti{},
			Storage:      &remoteStorage{},
			Uploads:      upload.NewRegistry(),
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
		if !slices.Equal(resp.Pending, resp.Paths) {
			t.Fatalf("expected the path to be pending, got %v", resp.Pending)
		}

		result, err := opts.Uploads.Wait(ctx, resp.Paths[0])
		if err != nil {
			t.Fatalf("failed to wait for upload: %v", err)
		}
		if result.Err != nil {
			t.Fatalf("upload failed: %v", result.Err)
		}
		if _, err := os.Stat(resp.Paths[0]); err != nil {
			t.Fatalf("tarball was not written: %v", err)
		}
		// The checksum is of the tarball as stored
		file, err := os.Open(resp.Paths[0])
		if err != nil {
			t.Fatal(err)
		}
		want, err := cedana_io.ChecksumOf(file)
		file.Close()
		if err != nil {
			t.Fatal(err)
		}
		if result.Checksum != want {
			t.Fatalf("checksum = %s, want %s", result.Checksum, want)
		}

		opts.WG.Wait()
	})

	t.Run("AsyncUploadFails", func(t *testing.T) {
		opts := types.Opts{
			WG:           &sync.WaitGroup{},
			CRIUCallback: &criu_client.NotifyCallbackMulti{},
			Storage:      &failingStorage{},
			Uploads:      upload.NewRegistry(),
		}
		req := &daemon.DumpReq{
			Dir:         t.TempDir(),
			Name:        fmt.Sprintf("dump-async-fails-%d", os.Getpid()),
			Compression: "lz4",
			Async:       true,
		}
		resp := &daemon.DumpResp{}

		// The dump itself succeeds, as the upload happens after it has returned
		_, err := DumpFilesystem(dumpImages(t))(ctx, opts, resp, req)
		if err != nil {
			t.Fatalf("dump failed: %v", err)
		}
		if !slices.Equal(resp.Pending, resp.Paths) {
			t.Fatalf("expected the path to be pending, got %v", resp.Pending)
		}

		result, err := opts.Uploads.Wait(ctx, resp.Paths[0])
		if err != nil {
			t.Fatalf("failed to wait for upload: %v", err)
		}
		if result.Err == nil {
			t.Fatal("expected the upload to fail")
		}

		opts.WG.Wait()
		os.RemoveAll(filepath.Join(os.TempDir(), req.Name))
	})

	t.Run("AsyncUntracked", func(t *testing.T) {
		opts := types.Opts{
			WG:           &sync.WaitGroup{},
			CRIUCallback: &criu_client.NotifyCallbackMulti{},
			Storage:      &remoteStorage{},
		}
		req := &daemon.DumpReq{
			Dir:         t.TempDir(),
			Name:        fmt.Sprintf("dump-async-untracked-%d", os.Getpid()),
			Compression: "lz4",
			Async:       true,
		}
		resp := &daemon.DumpResp{}

		_, err := DumpFilesystem(dumpImages(t))(ctx, opts, resp, req)
		if err != nil {
			t.Fatalf("dump failed: %v", err)
		}

		opts.WG.Wait()

		if _, err := os.Stat(resp.Paths[0]); err != nil {
			t.Fatalf("expected dump to be uploaded: %v", err)
		}
	})
}

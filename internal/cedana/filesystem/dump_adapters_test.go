package filesystem

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
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

// The images the fake dump writes
func fakeImages() map[string]string {
	images := map[string]string{}
	for i := range 3 {
		images[fmt.Sprintf("pages-%d.img", i)] = fmt.Sprintf("pages of image %d", i)
	}
	return images
}

// The manifest the mem-verify plugin writes for the fake images, and its checksum:
// the checkpoint's checksum, whatever the storage holds
func fakeManifest(t *testing.T) (text string, checksum string) {
	t.Helper()
	images := fakeImages()
	names := make([]string, 0, len(images))
	for name := range images {
		names = append(names, name)
	}
	slices.Sort(names)
	entries := make([]cedana_io.ManifestEntry, 0, len(names))
	for _, name := range names {
		entries = append(entries, cedana_io.ManifestEntry{Name: name, Checksum: sumOf(t, []byte(images[name]))})
	}
	text = manifestText(entries)
	return text, sumOf(t, []byte(text))
}

// Stands in for the CRIU dump with the mem-verify plugin. Writes images and the
// manifest to the images directory, notifies the manifest's checksum as the plugin
// does from CRIU's fini, and triggers the post-dump hook as CRIU would.
func dumpImages(t *testing.T) types.Dump {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (func() <-chan int, error) {
		imagesDirectory := req.GetCriu().GetImagesDir()
		for name, content := range fakeImages() {
			if err := os.WriteFile(filepath.Join(imagesDirectory, name), []byte(content), 0o644); err != nil {
				t.Fatalf("failed to write image: %v", err)
			}
		}
		manifest, checksum := fakeManifest(t)
		if err := os.WriteFile(filepath.Join(imagesDirectory, cedana_io.ManifestImage), []byte(manifest), 0o644); err != nil {
			t.Fatalf("failed to write the manifest: %v", err)
		}
		notice := fmt.Sprintf(`{"manifest":%q,"files":%d}`, checksum, strings.Count(manifest, "\n"))
		if err := opts.CRIUCallback.MemVerifyManifest(ctx, []byte(notice)); err != nil {
			t.Fatalf("manifest notice: %v", err)
		}
		return nil, opts.CRIUCallback.PostDump(ctx, req.GetCriu())
	}
}

// The CRIU dump under a CRIU without the plugin: images, no manifest, no notice
func dumpImagesWithoutManifest(t *testing.T) types.Dump {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (func() <-chan int, error) {
		imagesDirectory := req.GetCriu().GetImagesDir()
		for name, content := range fakeImages() {
			if err := os.WriteFile(filepath.Join(imagesDirectory, name), []byte(content), 0o644); err != nil {
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
		// The checksum is the manifest's, which the plugin notified during the dump
		if _, want := fakeManifest(t); result.Checksum != want {
			t.Fatalf("checksum = %s, want the manifest's %s", result.Checksum, want)
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

// A local tarball is compressed after the dump has returned, and its checksum, from
// the storage's writer, is held for the caller as an upload's outcome would be
// A synchronous local dump, with or without leave-running, holds its tarball's
// checksum the same way, known when the dump returns
// A tarball records the manifest's checksum, which the plugin notified during the
// dump, not the tarball's own; it is held for the caller once the compress has ended
func TestLocalSyncDumpHoldsTheManifestChecksum(t *testing.T) {
	config.Global.Checkpoint.Checksum = true
	ctx := context.Background()
	_, manifest := fakeManifest(t)
	for _, leaveRunning := range []bool{false, true} {
		opts := types.Opts{
			WG:           &sync.WaitGroup{},
			CRIUCallback: &criu_client.NotifyCallbackMulti{},
			Storage:      &Storage{},
			Uploads:      upload.NewRegistry(),
		}
		req := &daemon.DumpReq{
			Dir:         t.TempDir(),
			Name:        "dump-local-sync",
			Compression: "lz4",
			Criu:        &criu_proto.CriuOpts{LeaveRunning: proto.Bool(leaveRunning)},
		}
		resp := &daemon.DumpResp{}
		_, err := DumpFilesystem(dumpImages(t))(ctx, opts, resp, req)
		if err != nil {
			t.Fatalf("dump failed: %v", err)
		}
		// Without leave-running the compress runs in the post-dump callback, which the fake dump invokes
		if !slices.Equal(resp.Pending, resp.Paths) {
			t.Fatalf("leaveRunning=%v: expected the path to be pending for its checksum, got %v", leaveRunning, resp.Pending)
		}
		result, err := opts.Uploads.Wait(ctx, resp.Paths[0])
		if err != nil || result.Err != nil || result.Checksum != manifest {
			t.Fatalf("leaveRunning=%v: Wait = %+v, %v, want the manifest's checksum %s", leaveRunning, result, err, manifest)
		}
	}
}

func TestLocalAsyncDumpHoldsTheManifestChecksum(t *testing.T) {
	config.Global.Checkpoint.Checksum = true
	ctx := context.Background()
	opts := types.Opts{
		WG:           &sync.WaitGroup{},
		CRIUCallback: &criu_client.NotifyCallbackMulti{},
		Storage:      &Storage{},
		Uploads:      upload.NewRegistry(),
	}
	req := &daemon.DumpReq{Dir: t.TempDir(), Name: "dump-local-async", Compression: "lz4", Async: true}
	resp := &daemon.DumpResp{}

	_, err := DumpFilesystem(dumpImages(t))(ctx, opts, resp, req)
	if err != nil {
		t.Fatalf("dump failed: %v", err)
	}
	if !slices.Equal(resp.Pending, resp.Paths) {
		t.Fatalf("expected the local path to be pending for its compress, got %v", resp.Pending)
	}
	result, err := opts.Uploads.Wait(ctx, resp.Paths[0])
	if err != nil || result.Err != nil {
		t.Fatalf("Wait = %+v, %v", result, err)
	}
	if _, err := os.Stat(resp.Paths[0]); err != nil {
		t.Fatalf("tarball was not written: %v", err)
	}
	if _, want := fakeManifest(t); result.Checksum != want {
		t.Fatalf("checksum = %s, want the manifest's %s", result.Checksum, want)
	}
	opts.WG.Wait()
}

// Under a CRIU without the plugin no manifest is notified: the checkpoint is stored
// and held for the caller without a checksum
func TestDumpWithoutTheManifestRecordsNoChecksum(t *testing.T) {
	config.Global.Checkpoint.Checksum = true
	ctx := context.Background()
	opts := types.Opts{
		WG:           &sync.WaitGroup{},
		CRIUCallback: &criu_client.NotifyCallbackMulti{},
		Storage:      &Storage{},
		Uploads:      upload.NewRegistry(),
	}
	req := &daemon.DumpReq{Dir: t.TempDir(), Name: "dump-no-plugin", Compression: "lz4", Async: true}
	resp := &daemon.DumpResp{}

	_, err := DumpFilesystem(dumpImagesWithoutManifest(t))(ctx, opts, resp, req)
	if err != nil {
		t.Fatalf("dump failed: %v", err)
	}
	result, err := opts.Uploads.Wait(ctx, resp.Paths[0])
	if err != nil || result.Err != nil {
		t.Fatalf("Wait = %+v, %v", result, err)
	}
	if result.Checksum != "" {
		t.Fatalf("checksum = %q, want none without a manifest", result.Checksum)
	}
	opts.WG.Wait()
}

// Local storage whose manifest read fails as told
type unreadableStorage struct {
	Storage
	err error
}

func (s *unreadableStorage) ChecksumPath(ctx context.Context, path string) (string, error) {
	return "", s.err
}

// A manifest that cannot be read leaves the stored directory without a checksum,
// not failed, and not pending
func TestDirectoryManifestErrorLeavesTheCheckpointWithoutAChecksum(t *testing.T) {
	config.Global.Checkpoint.Checksum = true
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"ReadError", errors.New("read error")},
		{"NotThere", fmt.Errorf("failed to stat: %w", fs.ErrNotExist)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := types.Opts{
				WG:           &sync.WaitGroup{},
				CRIUCallback: &criu_client.NotifyCallbackMulti{},
				Storage:      &unreadableStorage{err: tc.err},
				Uploads:      upload.NewRegistry(),
			}
			req := &daemon.DumpReq{Dir: t.TempDir(), Name: "dump-dir", Compression: "none"}
			resp := &daemon.DumpResp{}

			_, err := DumpFilesystem(dumpImages(t))(ctx, opts, resp, req)
			if err != nil {
				t.Fatalf("dump failed: %v", err)
			}
			if len(resp.Pending) != 0 {
				t.Fatalf("a directory is never pending, got %v", resp.Pending)
			}
			if sum, ok := resp.Checksums[resp.Paths[0]]; ok {
				t.Fatalf("checksum = %q, want none", sum)
			}
			if _, err := opts.Uploads.Wait(ctx, resp.Paths[0]); !errors.Is(err, upload.ErrNotFound) {
				t.Fatalf("the registry must not know a directory, got %v", err)
			}
		})
	}
}

// A directory's checksum is in the response when the dump returns: the manifest the
// plugin wrote, read from the directory, which the helper reports ready at once
func TestDirectoryDumpHoldsTheManifestChecksum(t *testing.T) {
	config.Global.Checkpoint.Checksum = true
	ctx := context.Background()
	opts := types.Opts{
		WG:           &sync.WaitGroup{},
		CRIUCallback: &criu_client.NotifyCallbackMulti{},
		Storage:      &Storage{},
		Uploads:      upload.NewRegistry(),
	}
	req := &daemon.DumpReq{Dir: t.TempDir(), Name: "dump-dir", Compression: "none"}
	resp := &daemon.DumpResp{}

	_, err := DumpFilesystem(dumpImages(t))(ctx, opts, resp, req)
	if err != nil {
		t.Fatalf("dump failed: %v", err)
	}
	if len(resp.Pending) != 0 {
		t.Fatalf("a directory is complete when the dump returns, got pending %v", resp.Pending)
	}
	_, want := fakeManifest(t)
	if got := resp.Checksums[resp.Paths[0]]; got != want {
		t.Fatalf("Checksums[path] = %q, want the manifest's %q", got, want)
	}
	if fromFile, err := (&Storage{}).ChecksumPath(ctx, resp.Paths[0]); err != nil || fromFile != want {
		t.Fatalf("ChecksumPath = %q, %v, want %q", fromFile, err, want)
	}
	if _, err := opts.Uploads.Wait(ctx, resp.Paths[0]); !errors.Is(err, upload.ErrNotFound) {
		t.Fatalf("the registry must not know a directory, got %v", err)
	}
}

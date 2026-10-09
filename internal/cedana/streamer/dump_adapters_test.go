package streamer

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
	"github.com/cedana/cedana/internal/cedana/filesystem"
	"github.com/cedana/cedana/pkg/config"
	criu_client "github.com/cedana/cedana/pkg/criu"
	cedana_io "github.com/cedana/cedana/pkg/io"
	"github.com/cedana/cedana/pkg/plugins"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/pkg/upload"
)

// Local storage that reports itself as remote, and that can be made
// to fail the upload of one of the shards.
type remoteStorage struct {
	filesystem.Storage
	failOn string
}

func (s *remoteStorage) IsRemote() bool {
	return true
}

func (s *remoteStorage) Create(ctx context.Context, path string) (io.WriteCloser, error) {
	if s.failOn != "" && filepath.Base(path) == s.failOn {
		return nil, errors.New("storage unavailable")
	}
	if err := os.MkdirAll(filepath.Dir(path), filesystem.DUMP_DIR_PERMS); err != nil {
		return nil, err
	}
	return s.Storage.Create(ctx, path)
}

// Remote storage whose writers report a checksum other than that of the bytes
// written, as a store that holds other bytes than those sent would
type miscountingStorage struct {
	remoteStorage
}

func (s *miscountingStorage) Create(ctx context.Context, path string) (io.WriteCloser, error) {
	w, err := s.remoteStorage.Create(ctx, path)
	if err != nil {
		return nil, err
	}
	return &wrongChecksum{w}, nil
}

type wrongChecksum struct {
	io.WriteCloser
}

func (w *wrongChecksum) Checksum() string {
	return "crc32c:00000000"
}

// The checksum of a streamed dump as stored: that of the manifest of its shards
func manifestOf(t *testing.T, path string, streams int32, ext string) string {
	t.Helper()
	entries := make([]cedana_io.ManifestEntry, 0, streams)
	for i := range streams {
		name := fmt.Sprintf(IMG_FILE_FORMATTER, i) + ext
		file, err := os.Open(filepath.Join(path, name))
		if err != nil {
			t.Fatal(err)
		}
		sum, err := cedana_io.ChecksumOf(file)
		file.Close()
		if err != nil {
			t.Fatal(err)
		}
		entries = append(entries, cedana_io.ManifestEntry{Name: name, Checksum: sum})
	}
	return cedana_io.ManifestChecksum(entries)
}

// Every shard of a streamed dump, as stored
func shardsOf(t *testing.T, path string, streams int32, ext string) {
	t.Helper()
	for i := range streams {
		shard := fmt.Sprintf(IMG_FILE_FORMATTER, i) + ext
		if _, err := os.Stat(filepath.Join(path, shard)); err != nil {
			t.Fatalf("shard %s was not written: %v", shard, err)
		}
	}
}

// Plugin manager that reports the plugins required for streaming as installed
type streamingPlugins struct {
	plugins.Manager
	streamerBinary string
}

func (m *streamingPlugins) IsInstalled(name string) bool {
	return true
}

func (m *streamingPlugins) Get(name string) *plugins.Plugin {
	return &plugins.Plugin{
		Name:   name,
		Status: plugins.INSTALLED,
		Binaries: []plugins.Binary{{
			Name:       filepath.Base(m.streamerBinary),
			InstallDir: filepath.Dir(m.streamerBinary),
		}},
	}
}

func TestDumpFilesystem(t *testing.T) {
	config.Global.Checkpoint.Checksum = true
	streamerBinary := "/usr/local/bin/cedana-image-streamer"
	if _, err := os.Stat(streamerBinary); os.IsNotExist(err) {
		t.Skipf("streamer binary not found at %s, skipping integration test", streamerBinary)
	}

	ctx := context.Background()
	streams := int32(2)

	// Stands in for the CRIU dump, writing images through the streamer
	dumpImages := func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (func() <-chan int, error) {
		for i := range 5 {
			file, err := opts.DumpFs.Create(fmt.Sprintf("pages-%d.img", i))
			if err != nil {
				return nil, err
			}
			if _, err := file.Write([]byte(fmt.Sprintf("pages of image %d", i))); err != nil {
				file.Close()
				return nil, err
			}
			if err := file.Close(); err != nil {
				return nil, err
			}
		}
		return nil, nil
	}

	for _, compression := range []string{"none", "lz4"} {
		t.Run(compression, func(t *testing.T) {
			opts := types.Opts{
				WG:           &sync.WaitGroup{},
				CRIUCallback: &criu_client.NotifyCallbackMulti{},
				Storage:      &filesystem.Storage{},
				Plugins:      &streamingPlugins{streamerBinary: streamerBinary},
				Uploads:      upload.NewRegistry(),
			}
			req := &daemon.DumpReq{
				Dir:         t.TempDir(),
				Name:        "dump",
				Compression: compression,
				Streams:     streams,
			}
			resp := &daemon.DumpResp{}

			_, err := DumpFilesystem(streams)(dumpImages)(ctx, opts, resp, req)
			if err != nil {
				t.Fatalf("dump failed: %v", err)
			}

			if len(resp.Paths) != 1 {
				t.Fatalf("expected 1 path, got %v", resp.Paths)
			}

			ext, err := cedana_io.ExtForCompression(compression)
			if err != nil {
				t.Fatal(err)
			}
			shardsOf(t, resp.Paths[0], streams, ext)

			// The checksum of the shards as written is held for the caller as an
			// upload's outcome would be, so the path is pending
			if !slices.Equal(resp.Pending, resp.Paths) {
				t.Fatalf("expected the path to be pending for its checksum, got %v", resp.Pending)
			}
			result, err := opts.Uploads.Wait(ctx, resp.Paths[0])
			if err != nil || result.Err != nil {
				t.Fatalf("Wait = %+v, %v", result, err)
			}
			if want := manifestOf(t, resp.Paths[0], streams, ext); result.Checksum != want {
				t.Fatalf("checksum = %s, want the manifest of the shards %s", result.Checksum, want)
			}
		})
	}

	t.Run("Async", func(t *testing.T) {
		opts := types.Opts{
			WG:           &sync.WaitGroup{},
			CRIUCallback: &criu_client.NotifyCallbackMulti{},
			Storage:      &remoteStorage{},
			Plugins:      &streamingPlugins{streamerBinary: streamerBinary},
			Uploads:      upload.NewRegistry(),
		}
		req := &daemon.DumpReq{
			Dir:         t.TempDir(),
			Name:        fmt.Sprintf("dump-async-%d", os.Getpid()),
			Compression: "lz4",
			Streams:     streams,
			Async:       true,
		}
		resp := &daemon.DumpResp{}

		_, err := DumpFilesystem(streams)(dumpImages)(ctx, opts, resp, req)
		if err != nil {
			t.Fatalf("dump failed: %v", err)
		}

		if len(resp.Paths) != 1 {
			t.Fatalf("expected 1 path, got %v", resp.Paths)
		}
		if !slices.Equal(resp.Pending, resp.Paths) {
			t.Fatalf("expected the path to be pending, got %v", resp.Pending)
		}
		path := resp.Paths[0]

		result, err := opts.Uploads.Wait(ctx, path)
		if err != nil {
			t.Fatalf("failed to wait for upload: %v", err)
		}
		if result.Err != nil {
			t.Fatalf("upload failed: %v", result.Err)
		}
		shardsOf(t, path, streams, ".lz4")
		if want := manifestOf(t, path, streams, ".lz4"); result.Checksum != want {
			t.Fatalf("checksum = %s, want the manifest of the uploaded shards %s", result.Checksum, want)
		}

		opts.WG.Wait()
	})

	// Open question 27: the uploaded shards' values are recorded as the store's
	// writers report them; a difference from the shards written is logged, not
	// a failure
	t.Run("AsyncUploadedValuesDiffer", func(t *testing.T) {
		opts := types.Opts{
			WG:           &sync.WaitGroup{},
			CRIUCallback: &criu_client.NotifyCallbackMulti{},
			Storage:      &miscountingStorage{},
			Plugins:      &streamingPlugins{streamerBinary: streamerBinary},
			Uploads:      upload.NewRegistry(),
		}
		req := &daemon.DumpReq{
			Dir:         t.TempDir(),
			Name:        fmt.Sprintf("dump-async-differs-%d", os.Getpid()),
			Compression: "lz4",
			Streams:     streams,
			Async:       true,
		}
		resp := &daemon.DumpResp{}

		_, err := DumpFilesystem(streams)(dumpImages)(ctx, opts, resp, req)
		if err != nil {
			t.Fatalf("dump failed: %v", err)
		}
		path := resp.Paths[0]

		result, err := opts.Uploads.Wait(ctx, path)
		if err != nil {
			t.Fatalf("failed to wait for upload: %v", err)
		}
		if result.Err != nil {
			t.Fatalf("the upload must stand, got: %v", result.Err)
		}
		shardsOf(t, path, streams, ".lz4")
		entries := make([]cedana_io.ManifestEntry, 0, streams)
		for i := range streams {
			entries = append(entries, cedana_io.ManifestEntry{Name: fmt.Sprintf(IMG_FILE_FORMATTER, i) + ".lz4", Checksum: "crc32c:00000000"})
		}
		if want := cedana_io.ManifestChecksum(entries); result.Checksum != want {
			t.Fatalf("checksum = %s, want the manifest of the values the store's writers reported %s", result.Checksum, want)
		}
		if local := manifestOf(t, path, streams, ".lz4"); result.Checksum == local {
			t.Fatalf("the checksum must be the uploaded values, not the shards written")
		}

		opts.WG.Wait()
	})

	t.Run("AsyncUploadFails", func(t *testing.T) {
		opts := types.Opts{
			WG:           &sync.WaitGroup{},
			CRIUCallback: &criu_client.NotifyCallbackMulti{},
			Storage:      &remoteStorage{failOn: fmt.Sprintf(IMG_FILE_FORMATTER, 1) + ".lz4"},
			Plugins:      &streamingPlugins{streamerBinary: streamerBinary},
			Uploads:      upload.NewRegistry(),
		}
		req := &daemon.DumpReq{
			Dir:         t.TempDir(),
			Name:        fmt.Sprintf("dump-async-fails-%d", os.Getpid()),
			Compression: "lz4",
			Streams:     streams,
			Async:       true,
		}
		resp := &daemon.DumpResp{}

		// An earlier checkpoint at the same path: the shard this upload fails to
		// create is the earlier checkpoint's, and the cleanup must not touch it
		earlier := filepath.Join(req.Dir, req.Name, fmt.Sprintf(IMG_FILE_FORMATTER, 1)+".lz4")
		if err := os.MkdirAll(filepath.Dir(earlier), filesystem.DUMP_DIR_PERMS); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(earlier, []byte("earlier checkpoint"), 0o644); err != nil {
			t.Fatal(err)
		}

		// The dump itself succeeds, as the upload happens after it has returned
		_, err := DumpFilesystem(streams)(dumpImages)(ctx, opts, resp, req)
		if err != nil {
			t.Fatalf("dump failed: %v", err)
		}
		path := resp.Paths[0]

		result, err := opts.Uploads.Wait(ctx, path)
		if err != nil {
			t.Fatalf("failed to wait for upload: %v", err)
		}
		if result.Err == nil {
			t.Fatal("expected the upload to fail")
		}

		opts.WG.Wait()

		// The shards this upload wrote must not be left behind; the one it did not
		// create belongs to the earlier checkpoint and stays
		shards, err := filepath.Glob(filepath.Join(path, "img-*"))
		if err != nil {
			t.Fatal(err)
		}
		if len(shards) != 1 || shards[0] != earlier {
			t.Fatalf("expected only the earlier checkpoint's shard %s after a failed upload, got %v", earlier, shards)
		}
		if content, _ := os.ReadFile(earlier); string(content) != "earlier checkpoint" {
			t.Fatalf("the earlier checkpoint's shard was changed: %q", content)
		}
	})
}

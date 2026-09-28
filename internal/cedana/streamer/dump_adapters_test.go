package streamer

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
	"github.com/cedana/cedana/internal/cedana/filesystem"
	criu_client "github.com/cedana/cedana/pkg/criu"
	cedana_io "github.com/cedana/cedana/pkg/io"
	"github.com/cedana/cedana/pkg/plugins"
	"github.com/cedana/cedana/pkg/types"
)

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

func TestDumpFilesystemChecksum(t *testing.T) {
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
			if len(resp.Checksums) != 1 {
				t.Fatalf("expected 1 checksum, got %v", resp.Checksums)
			}

			ext, err := cedana_io.ExtForCompression(compression)
			if err != nil {
				t.Fatal(err)
			}

			for path, checksum := range resp.Checksums {
				if !slices.Contains(resp.Paths, path) {
					t.Fatalf("checksum is for %s, which is not in paths %v", path, resp.Paths)
				}

				// Recompute from the shards as stored
				var manifest string
				for i := range streams {
					shard := fmt.Sprintf(IMG_FILE_FORMATTER, i) + ext
					data, err := os.ReadFile(filepath.Join(path, shard))
					if err != nil {
						t.Fatalf("failed to read shard %s: %v", shard, err)
					}
					sum := sha256.Sum256(data)
					manifest += shard + " sha256:" + hex.EncodeToString(sum[:]) + "\n"
				}
				sum := sha256.Sum256([]byte(manifest))
				if expected := "sha256:" + hex.EncodeToString(sum[:]); checksum != expected {
					t.Fatalf("checksum is %s, expected %s", checksum, expected)
				}
			}
		})
	}
}

package streamer

import (
	"context"
	"errors"
	"os"
	"sync"
	"syscall"
	"time"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/internal/cedana/verify"
	criu_client "github.com/cedana/cedana/pkg/criu"
	cedana_io "github.com/cedana/cedana/pkg/io"
	"github.com/cedana/cedana/pkg/plugins"
	"github.com/cedana/cedana/pkg/profiling"
	"github.com/cedana/cedana/pkg/types"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// How long the post-restore check waits for the last shard to be read to its end.
// CRIU has read every image by then; only the end of a shard can be in flight.
var SHARDS_READ_TIMEOUT = 10 * time.Second

func RestoreFilesystem(streams int32) types.Adapter[types.Restore] {
	return func(next types.Restore) types.Restore {
		return func(ctx context.Context, opts types.Opts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (code func() <-chan int, err error) {
			storage := opts.Storage
			path := req.GetPath()

			// The checksum of a streamed checkpoint is the manifest of its shards. The
			// shards feed CRIU as they are read, so the manifest is complete only when
			// CRIU has read every image: it is compared at CRIU's post-restore
			// notification, when the tasks are restored and not yet running. A strict
			// mismatch kills them there and fails the restore, so the process never runs
			// and the runtime keeps no state of the container.
			check := verify.For(req)
			var store string
			if check != nil {
				ctx = WithShardChecksums(ctx)
				if storage.IsRemote() {
					if paths, err := imgPaths(ctx, storage, path, READ_ONLY, streams); err == nil {
						store = storeManifest(ctx, storage, paths)
					}
					if check.StoreDiffers(store) && check.Strict {
						return nil, check.Record(resp, check.Stored(store))
					}
				}
			}

			var imagesDirectory string

			if !storage.IsRemote() {
				stat, err := os.Stat(path)
				if err != nil {
					return nil, status.Errorf(codes.NotFound, "path error: %s", path)
				}
				if !stat.IsDir() {
					return nil, status.Errorf(codes.InvalidArgument, "path must be a directory containing streamed images")
				}
				imagesDirectory = path
			} else {
				// For remote storage, we create a temporary directory for CRIU
				imagesDirectory, err = os.MkdirTemp("", "restore-\\*")
				if err != nil {
					return nil, status.Errorf(codes.Internal, "failed to create temp restore dir: %v", err)
				}
				defer os.RemoveAll(imagesDirectory)
			}

			// Streamer also requires Cedana's CRIU version until the Stream proto option
			// is merged into CRIU upstream.
			if !opts.Plugins.IsInstalled("criu") {
				return nil, status.Errorf(
					codes.FailedPrecondition,
					"Streaming C/R requires the CRIU plugin to be installed. Default CRIU is not supported yet.",
				)
			}

			dir, err := os.Open(imagesDirectory)
			if err != nil {
				os.RemoveAll(imagesDirectory)
				return nil, status.Errorf(codes.Internal, "failed to open dump dir: %v", err)
			}
			defer dir.Close()

			if req.GetCriu() == nil {
				req.Criu = &criu_proto.CriuOpts{}
			}

			req.Criu.ImagesDir = proto.String(imagesDirectory)
			req.Criu.ImagesDirFd = proto.Int32(int32(dir.Fd()))
			req.Criu.Stream = proto.Bool(true)

			// Setup dump fs that can be used by future adapters to directly read write/extra files
			// to the dump directory. Here, instead of OsFs we use the streamer's Fs implementation
			// that handles all read/writes directly through streaming.
			var imgStreamer *plugins.Plugin
			if imgStreamer = opts.Plugins.Get("streamer"); !imgStreamer.IsInstalled() {
				return nil, status.Errorf(
					codes.FailedPrecondition,
					"Provided checkpoint path requires streaming. Please install the streamer plugin to use streaming C/R",
				)
			}

			// Setup filesystem that can be used by future adapters to directly read files from the checkpoint

			var waitForIO func() error
			opts.DumpFs, waitForIO, err = NewStreamingFs(
				ctx,
				imgStreamer.BinaryPaths()[0],
				imagesDirectory,
				storage,
				req.Path,
				streams,
				READ_ONLY,
			)
			if err != nil {
				return nil, status.Errorf(codes.Internal, "failed to create streaming fs: %v", err)
			}
			streamFs := opts.DumpFs.(*Fs)

			// Verified once: at post-restore when every shard has been read by then,
			// otherwise after the restore
			var verifyOnce sync.Once
			var verifyErr error
			verified := false
			verifyNow := func() error {
				verifyOnce.Do(func() {
					verified = true
					manifest := streamFs.Manifest()
					if manifest == "" {
						log.Warn().Str("path", path).Msg("a shard was not read to its end, checkpoint checksum not verified")
						return
					}
					verifyErr = check.Record(resp, check.Compare(manifest, store, !storage.IsRemote()))
				})
				return verifyErr
			}
			if check != nil {
				// go-criu dispatches post-restore, not pre-resume; the tasks are restored
				// and stopped at both
				opts.CRIUCallback.Include(&criu_client.NotifyCallback{
					Name: "checksum",
					PostRestoreFunc: func(ctx context.Context, pid int32) error {
						select {
						case <-streamFs.ShardsRead():
						case <-time.After(SHARDS_READ_TIMEOUT):
							log.Warn().Msg("shards still being read at post-restore, the checksum is verified after the restore")
							return nil
						}
						log.Debug().Int32("pid", pid).Msg("verifying the streamed checkpoint before the restored tasks run")
						err := verifyNow()
						if err != nil && pid > 0 {
							// The tasks have not run; they must not
							syscall.Kill(int(pid), syscall.SIGKILL)
						}
						return err
					},
				})
			}

			defer func() {
				_, end := profiling.StartTimingCategory(ctx, "storage", waitForIO)
				err = errors.Join(err, waitForIO())
				end()

				// A failed verification is the restore's error, not CRIU's report of it
				if verifyErr != nil {
					err = verifyErr
					return
				}

				// The restore went through without a post-restore check: verify now, and
				// in strict mode stop the restored process on a mismatch
				if check != nil && err == nil && !verified {
					if verr := verifyNow(); verr != nil {
						if resp.PID > 0 {
							syscall.Kill(int(resp.PID), syscall.SIGKILL)
						}
						err = verr
					}
				}
			}()

			return next(ctx, opts, resp, req)
		}
	}
}

// storeManifest returns the manifest of the store's own values of the shards, named
// and ordered as the dump names them, or "" if any shard has none
func storeManifest(ctx context.Context, storage cedana_io.Storage, paths []string) string {
	values := make([]string, len(paths))
	for i, path := range paths {
		if values[i] = verify.StoreValue(ctx, storage, path); values[i] == "" {
			return ""
		}
	}
	return shardManifest(paths, values)
}

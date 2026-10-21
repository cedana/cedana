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
// CRIU has read every image by then; only the end of a shard can be in flight. In
// warn mode the check moves after the restore when the wait runs out; in strict
// mode the restored tasks must not run unverified, so the wait is long and running
// out fails the restore and cancels the reads: a failed restore does not wait on
// a stalled download.
var (
	SHARDS_READ_TIMEOUT        = 10 * time.Second
	SHARDS_READ_TIMEOUT_STRICT = 5 * time.Minute
)

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

			// The shards are read under their own context, cancelled when the strict
			// check gives up on them: the restore has failed by then, and the wait for
			// the reads below must not hang on a download that stalls
			ioCtx, cancelIO := context.WithCancel(ctx)
			defer cancelIO()

			var waitForIO func() error
			opts.DumpFs, waitForIO, err = NewStreamingFs(
				ioCtx,
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
					verifyErr = verifyManifest(check, resp, streamFs.Manifest(), store, path, !storage.IsRemote())
				})
				return verifyErr
			}
			if check != nil {
				// go-criu dispatches post-restore, not pre-resume; the tasks are restored
				// and stopped at both
				opts.CRIUCallback.Include(&criu_client.NotifyCallback{
					Name: "checksum",
					PostRestoreFunc: func(ctx context.Context, pid int32) error {
						return checksumPostRestore(streamFs.ShardsRead(), check.Strict, pid, verifyNow, func(err error) {
							verifyOnce.Do(func() {
								verified = true
								verifyErr = err
							})
							cancelIO()
						})
					},
				})
			}

			defer func() {
				_, end := profiling.StartTimingCategory(ctx, "storage", waitForIO)
				err = errors.Join(err, waitForIO())
				end()

				// A failed verification is the restore's error, not CRIU's report of it;
				// a failed read of the shards explains a checksum that was not verified,
				// unless the check gave up on the reads and cancelled them
				if verifyErr != nil {
					if err != nil && !errors.Is(err, context.Canceled) {
						err = status.Errorf(status.Code(verifyErr), "%s: %v", status.Convert(verifyErr).Message(), err)
						return
					}
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

// verifyManifest compares the manifest of the shards as read with the expected
// checksum and records the outcome. An empty manifest says a shard failed before
// its end, so its checksum is unknown: strict mode fails the restore, it does not
// restore what it cannot verify; warn mode restores unverified.
func verifyManifest(check *verify.Check, resp *daemon.RestoreResp, manifest, store, path string, local bool) error {
	if manifest == "" {
		if check.Strict {
			return status.Errorf(codes.FailedPrecondition, "checkpoint checksum not verified: a shard of %s was not read to its end", path)
		}
		log.Warn().Str("path", path).Msg("a shard was not read to its end, checkpoint checksum not verified")
		return nil
	}
	return check.Record(resp, check.Compare(manifest, store, local))
}

// checksumPostRestore is the check at CRIU's post-restore notification, when the tasks are
// restored and not yet running. It waits for the last shard to be read to its end, then
// verifies; a strict mismatch kills the tasks and fails the restore. When the wait runs
// out, warn mode lets the tasks run and verifies after the restore, and strict mode gives
// up on the shards, kills the tasks and fails the restore: they must not run unverified.
func checksumPostRestore(shardsRead <-chan struct{}, strict bool, pid int32, verifyNow func() error, giveUp func(error)) error {
	timeout := SHARDS_READ_TIMEOUT
	if strict {
		timeout = SHARDS_READ_TIMEOUT_STRICT
	}
	select {
	case <-shardsRead:
	case <-time.After(timeout):
		if !strict {
			log.Warn().Msg("shards still being read at post-restore, the checksum is verified after the restore")
			return nil
		}
		err := status.Errorf(codes.FailedPrecondition,
			"checkpoint checksum not verified: the shards were still being read %s after the restore", timeout)
		giveUp(err)
		if pid > 0 {
			syscall.Kill(int(pid), syscall.SIGKILL)
		}
		return err
	}
	log.Debug().Int32("pid", pid).Msg("verifying the streamed checkpoint before the restored tasks run")
	err := verifyNow()
	if err != nil && pid > 0 {
		// The tasks have not run; they must not
		syscall.Kill(int(pid), syscall.SIGKILL)
	}
	return err
}

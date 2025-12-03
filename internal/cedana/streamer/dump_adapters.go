package streamer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/internal/cedana/filesystem"
	"github.com/cedana/cedana/pkg/config"
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

// This adapter sets up the streaming filesystem for the dump, that uses the cedana-image-streamer as the backend.
func DumpFilesystem(streams int32) types.Adapter[types.Dump] {
	return func(next types.Dump) types.Dump {
		return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (code func() <-chan int, err error) {
			storage := opts.Storage
			dir := req.Dir
			compression := req.Compression
			if compression == "" {
				compression = config.Global.Checkpoint.Compression
			}

			// Check if compression is valid, because we don't want to fail after the dump
			// as the process would be killed.

			if _, ok := cedana_io.SUPPORTED_COMPRESSIONS[compression]; !ok {
				return nil, status.Errorf(codes.Unimplemented, "unsupported compression format '%s'", compression)
			}

			async := config.Global.Checkpoint.Async && storage.IsRemote()

			// If remote storage, we instead use a temporary directory for CRIU
			if storage.IsRemote() {
				dir = os.TempDir()
			}

			// Check if the provided dir exists
			if _, err := os.Stat(dir); os.IsNotExist(err) {
				return nil, status.Errorf(codes.InvalidArgument, "dump dir does not exist: %s", dir)
			}

			// Create a new directory within the dump dir, where dump will happen
			imagesDirectory := filepath.Join(dir, req.Name)

			// Create the directory
			if err := os.Mkdir(imagesDirectory, filesystem.DUMP_DIR_PERMS); err != nil {
				return nil, status.Errorf(codes.Internal, "failed to create dump dir: %v", err)
			}
			defer func() {
				if err != nil || (storage.IsRemote() && !async) {
					os.RemoveAll(imagesDirectory)
				}
			}()
			err = os.Chmod(imagesDirectory, filesystem.DUMP_DIR_PERMS)
			if err != nil {
				return nil, status.Errorf(codes.Internal, "failed to chmod dump dir: %v", err)
			}

			f, err := os.Open(imagesDirectory)
			if err != nil {
				return nil, status.Errorf(codes.Internal, "failed to open dump dir: %v", err)
			}
			defer f.Close()

			if req.GetCriu() == nil {
				req.Criu = &criu_proto.CriuOpts{}
			}

			req.Criu.ImagesDir = proto.String(imagesDirectory)
			req.Criu.ImagesDirFd = proto.Int32(int32(f.Fd()))
			req.Criu.Stream = proto.Bool(true)

			// Streamer also requires Cedana's CRIU version until the Stream proto option
			// is merged into CRIU upstream.
			if !opts.Plugins.IsInstalled("criu") {
				return nil, status.Errorf(
					codes.FailedPrecondition,
					"Streaming C/R requires the CRIU plugin to be installed. Default CRIU is not supported yet.",
				)
			}

			var imgStreamer *plugins.Plugin
			if imgStreamer = opts.Plugins.Get("streamer"); !imgStreamer.IsInstalled() {
				return nil, status.Errorf(
					codes.FailedPrecondition,
					"Please install the streamer plugin to use streaming C/R",
				)
			}

			path := req.Dir + "/" + req.Name // do not use filepath.Join as it removes a slash

			var streamStorage cedana_io.Storage
			var storagePath string

			if async {
				streamStorage = &filesystem.Storage{}
				storagePath = imagesDirectory
			} else {
				streamStorage = storage
				storagePath = path
			}

			var waitForIO func() error
			opts.DumpFs, waitForIO, err = NewStreamingFs(
				ctx,
				imgStreamer.BinaryPaths()[0],
				imagesDirectory,
				streamStorage,
				storagePath,
				streams,
				WRITE_ONLY,
				compression,
			)
			if err != nil {
				return nil, status.Errorf(codes.Internal, "failed to create streaming fs: %v", err)
			}

			// XXX: We do not differentiate between leave-running or not, because unfortunately CRIU
			// does not close the streaming file descriptors on its side when the PostDumpFunc is triggered.
			// This is why the logic here is not the same as that in `filesystem/dump_adapters.go`
			if async {
				ext, _ := cedana_io.ExtForCompression(compression)

				upload := func(ctx context.Context) error {
					var wg sync.WaitGroup
					errCh := make(chan error, streams)

					for i := int32(0); i < streams; i++ {
						wg.Add(1)
						go func(i int32) {
							defer wg.Done()

							localPath := fmt.Sprintf("%s/img-%d%s", imagesDirectory, i, ext)
							remotePath := fmt.Sprintf("%s/img-%d%s", path, i, ext)

							src, err := streamStorage.Open(localPath)
							if err != nil {
								errCh <- fmt.Errorf("failed to open local shard %d: %w", i, err)
								return
							}
							defer src.Close()

							dst, err := storage.Create(remotePath)
							if err != nil {
								errCh <- fmt.Errorf("failed to create remote shard %d: %w", i, err)
								return
							}
							defer dst.Close()

							if _, err := io.Copy(dst, src); err != nil {
								errCh <- fmt.Errorf("failed to upload shard %d: %w", i, err)
							}
						}(i)
					}

					wg.Wait()
					close(errCh)

					var uploadErr error
					for e := range errCh {
						uploadErr = errors.Join(uploadErr, e)
					}

					os.RemoveAll(imagesDirectory)
					return uploadErr
				}

				defer func() {
					_, end := profiling.StartTimingCategory(ctx, "storage", waitForIO)
					err = errors.Join(err, waitForIO())
					end()

					if err != nil {
						return
					}

					// Use a detached context for async upload since the parent request
					// context will be canceled after the dump completes.
					uploadCtx := context.WithoutCancel(ctx)

					if req.GetCriu().GetLeaveRunning() {
						opts.WG.Add(1)
						go func() {
							defer opts.WG.Done()
							asyncCtx, end := profiling.StartTiming(uploadCtx, "async-upload")
							defer end()
							if uploadErr := upload(asyncCtx); uploadErr != nil {
								log.Error().Err(uploadErr).Msg("async upload failed")
							}
						}()
					} else {
						callback := &criu_client.NotifyCallback{
							PostDumpFunc: func(_ context.Context, _ *criu_proto.CriuOpts) error {
								asyncCtx, end := profiling.StartTiming(uploadCtx, "async-upload")
								defer end()
								return upload(asyncCtx)
							},
						}
						opts.CRIUCallback.Include(callback)
					}
				}()
			} else {
				defer func() {
					_, end := profiling.StartTimingCategory(ctx, "storage", waitForIO)
					err = errors.Join(err, waitForIO())
					end()
				}()
			}

			resp.Paths = append(resp.Paths, path)

			return next(ctx, opts, resp, req)
		}
	}
}

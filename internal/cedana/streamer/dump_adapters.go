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

			if _, ok := cedana_io.SUPPORTED_COMPRESSIONS[compression]; !ok {
				return nil, status.Errorf(codes.Unimplemented, "unsupported compression format '%s'", compression)
			}

			staged := config.Global.Checkpoint.Staged && storage.IsRemote()

			if storage.IsRemote() {
				dir = os.TempDir()
			}

			if _, err := os.Stat(dir); os.IsNotExist(err) {
				return nil, status.Errorf(codes.InvalidArgument, "dump dir does not exist: %s", dir)
			}

			imagesDirectory := filepath.Join(dir, req.Name)

			if err := os.Mkdir(imagesDirectory, filesystem.DUMP_DIR_PERMS); err != nil {
				return nil, status.Errorf(codes.Internal, "failed to create dump dir: %v", err)
			}
			defer func() {
				if err != nil || (storage.IsRemote() && !staged) {
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

			path := req.Dir + "/" + req.Name

			var streamStorage cedana_io.Storage
			var storagePath string
			if staged {
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

			if staged {
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

					if req.GetCriu().GetLeaveRunning() {
						opts.WG.Add(1)
						go func() {
							defer opts.WG.Done()
							if uploadErr := upload(ctx); uploadErr != nil {
								log.Error().Err(uploadErr).Msg("staged upload failed")
							}
						}()
					} else {
						callback := &criu_client.NotifyCallback{
							PostDumpFunc: func(ctx context.Context, _ *criu_proto.CriuOpts) error {
								return upload(ctx)
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

package filesystem

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/pkg/config"
	criu_client "github.com/cedana/cedana/pkg/criu"
	"github.com/cedana/cedana/pkg/io"
	"github.com/cedana/cedana/pkg/profiling"
	"github.com/cedana/cedana/pkg/types"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

const DUMP_DIR_PERMS = 0o755

// This adapter uses the provided storage to setup the dump.
// Compresses the dump directory post-dump, based on a compression format:
//   - "none" does not compress the dump directory
//   - "tar" creates a tarball of the dump directory
//   - "gzip" creates a gzipped tarball of the dump directory
//   - "lz4" creates an lz4-compressed tarball of the dump directory
func DumpFilesystem(next types.Dump) types.Dump {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (code func() <-chan int, err error) {
		storage := opts.Storage
		dir := req.Dir
		compression := req.Compression

		if compression == "" {
			compression = config.Global.Checkpoint.Compression
		}

		if _, ok := io.SUPPORTED_COMPRESSIONS[compression]; !ok {
			return nil, status.Errorf(codes.Unimplemented, "unsupported compression format '%s'", compression)
		}

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
		if err := os.Mkdir(imagesDirectory, DUMP_DIR_PERMS); err != nil {
			return nil, status.Errorf(codes.Internal, "failed to create dump dir: %v", err)
		}
		defer func() {
			if err != nil {
				os.RemoveAll(imagesDirectory)
			}
		}()
		err = os.Chmod(imagesDirectory, DUMP_DIR_PERMS) // XXX: Because for some reason mkdir is not applying perms
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

		// Setup dump fs that can be used by future adapters to directly read write/extra files
		// to the dump directory
		opts.DumpFs = afero.NewBasePathFs(afero.NewOsFs(), imagesDirectory)

		// If remote storage, or compression needs to be done, we do it in CRIU's post-dump hook
		// so that if we fail compression/upload, CRIU can still resume the process (only if leave-running is not set)

		if storage.IsRemote() || (compression != "" && compression != "none") {
			compress := func() (err error) {
				var path, ext string
				ext, err = io.ExtForCompression(compression)
				if err != nil {
					return err
				}

				path = req.Dir + "/" + req.Name + ".tar" + ext // do not use filepath.Join as it removes a slash (for remote)

				tarball, err := storage.Create(path)
				if err != nil {
					return fmt.Errorf("failed to create tarball in storage: %w", err)
				}
				defer func() {
					err = errors.Join(err, tarball.Close())
				}()

				log.Debug().Str("path", path).Str("compression", compression).Msg("creating tarball")

				_, end := profiling.StartTimingCategory(ctx, "storage", io.Tar)
				err = io.Tar(imagesDirectory, tarball, compression)
				end()
				if err != nil {
					storage.Delete(path)
					return fmt.Errorf("failed to create tarball: %w", err)
				}

				log.Debug().Str("path", path).Str("compression", compression).Msg("created tarball")

				os.RemoveAll(imagesDirectory)
				resp.Path = path
				return nil
			}

			// If leave-running is requested, then we do not to block process for compress/upload, because the process
			// will continue running regardless of the success of the dump/compress/upload. If leave-running is not set,
			// then we need to ensure that the dump is compressed/uploaded in the post-dump hook so that it
			// can be resumed on failure.

			if req.GetCriu().GetLeaveRunning() {
				defer func() {
					err = errors.Join(err, compress())
				}()
			} else {
				callback := &criu_client.NotifyCallback{
					PostDumpFunc: func(_ context.Context, _ *criu_proto.CriuOpts) (err error) {
						return compress()
					},
				}
				opts.CRIUCallback.Include(callback)
			}
		} else {
			// Nothing else to do, just set the path
			resp.Path = imagesDirectory
		}

		return next(ctx, opts, resp, req)
	}
}

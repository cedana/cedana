package filesystem

import (
	"context"
	"os"
	"path/filepath"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/pkg/config"
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
func SetupDumpFS(storage io.Storage) types.Adapter[types.Dump] {
	return func(next types.Dump) types.Dump {
		return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (exited chan int, err error) {
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

			exited, err = next(ctx, opts, resp, req)
			if err != nil {
				return nil, err
			}

			// If nothing was put in the directory, remove it and return early
			entries, err := os.ReadDir(imagesDirectory)
			if err != nil {
				return exited, status.Errorf(codes.Internal, "failed to read dump dir: %v", err)
			}
			if len(entries) == 0 {
				os.RemoveAll(imagesDirectory)
				return exited, nil
			}

			resp.Path = imagesDirectory

			// If not remote storage, we can just return here
			// If remote storage, we need to still at least tar the directory even if no compression specified

			if !storage.IsRemote() && (compression == "" || compression == "none") {
				return exited, err // Nothing else to do
			}

			// Create the tarball

			var path string

			ext, _ := io.ExtForCompression(compression)

			if storage.IsRemote() {
				path = req.Dir + "/" + req.Name + ".tar" + ext // do not use filepath.Join as it removes a slash
			} else {
				path = imagesDirectory + ".tar" + ext
			}

			log.Debug().Str("path", path).Str("compression", compression).Msg("creating tarball")

			tarball, err := storage.Create(path)
			if err != nil {
				return exited, status.Errorf(codes.Internal, "failed to create tarball file: %v", err)
			}
			defer tarball.Close()

			log.Debug().Str("path", path).Str("compression", compression).Msg("creating tarball")

			_, end := profiling.StartTimingCategory(ctx, "storage", io.Tar)
			err = io.Tar(imagesDirectory, tarball, compression)
			end()
			if err != nil {
				storage.Delete(path)
				return exited, status.Errorf(codes.Internal, "failed to tarball into storage: %v", err)
			}

			log.Debug().Str("path", path).Str("compression", compression).Msg("created tarball")

			os.RemoveAll(imagesDirectory)

			resp.Path = path

			return exited, nil
		}
	}
}

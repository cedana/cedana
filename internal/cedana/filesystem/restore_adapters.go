package filesystem

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/pkg/io"
	"github.com/cedana/cedana/pkg/profiling"
	"github.com/cedana/cedana/pkg/types"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// This adapter decompresses (if required) the dump to a temporary directory for restore.
// Automatically detects the compression format from the file extension.
func RestoreFilesystem(next types.Restore) types.Restore {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (code func() <-chan int, err error) {
		storage := opts.Storage
		path := req.GetPath()

		var isDir bool
		var imagesDirectory string

		if !storage.IsRemote() {
			stat, err := os.Stat(path)
			if err != nil {
				return nil, status.Errorf(codes.NotFound, "path error: %s", path)
			}
			isDir = stat.IsDir()
		}

		// If not remote storage, and path is directory, we can directly use it for CRIU

		if !storage.IsRemote() && isDir {
			imagesDirectory = path
		} else {
			// Create a temporary directory for the restore
			imagesDirectory = filepath.Join(os.TempDir(), fmt.Sprintf("restore-%d", time.Now().UnixNano()))

			if err := os.Mkdir(imagesDirectory, DUMP_DIR_PERMS); err != nil {
				return nil, status.Errorf(codes.Internal, "failed to create restore dir: %v", err)
			}
			err = os.Chmod(imagesDirectory, DUMP_DIR_PERMS) // XXX: Because for some reason mkdir is not applying perms
			if err != nil {
				return nil, status.Errorf(codes.Internal, "failed to chmod restore dir: %v", err)
			}
			defer os.RemoveAll(imagesDirectory)

			decompress := func(ctx context.Context) (err error) {
				// Detect compression from path
				compression, err := io.CompressionFromExt(path)
				if err != nil {
					return err
				}

				tarball, err := storage.Open(ctx, path)
				if err != nil {
					return fmt.Errorf("failed to open dump file: %v", err)
				}
				defer func() {
					err = errors.Join(err, tarball.Close())
				}()

				log.Debug().Str("path", path).Str("compression", compression).Msg("decompressing tarball")

				tarball = profiling.IOCategory(ctx, tarball, "storage", io.Untar, compression)
				err = io.Untar(tarball, imagesDirectory, compression)
				if err != nil {
					return fmt.Errorf("failed to decompress dump: %v", err)
				}

				log.Debug().Str("path", path).Str("compression", compression).Msg("decompressed tarball")

				return nil
			}

			err = decompress(ctx)
			if err != nil {
				return nil, status.Error(codes.Internal, err.Error())
			}
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

		// Setup dump fs that can be used by future adapters to directly read files
		// to the dump directory
		opts.DumpFs = afero.NewBasePathFs(afero.NewOsFs(), imagesDirectory)

		return next(ctx, opts, resp, req)
	}
}

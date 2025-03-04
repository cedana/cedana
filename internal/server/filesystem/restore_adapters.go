package filesystem

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/pkg/profiling"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// This adapter decompresses (if required) the dump to a temporary directory for restore.
// Automatically detects the compression format from the file extension.
func PrepareDumpDirForRestore(next types.Restore) types.Restore {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (chan int, error) {
		path := req.GetPath()
		stat, err := os.Stat(path)
		if err != nil {
			return nil, status.Errorf(codes.NotFound, "path error: %s", path)
		}

		var dir *os.File
		var imagesDirectory string

		if stat.IsDir() {
			imagesDirectory = path
		} else {
			// Create a temporary directory for the restore
			imagesDirectory = filepath.Join(os.TempDir(), fmt.Sprintf("restore-%d", time.Now().UnixNano()))
			if err := os.Mkdir(imagesDirectory, DUMP_DIR_PERMS); err != nil {
				return nil, status.Errorf(codes.Internal, "failed to create restore dir: %v", err)
			}
			err = os.Chmod(imagesDirectory, DUMP_DIR_PERMS) // XXX: Because for some reason mkdir is not applying perms
			if err != nil {
				return nil, status.Errorf(codes.Internal, "failed to chmod dump dir: %v", err)
			}
			defer os.RemoveAll(imagesDirectory)

			log.Debug().Str("path", path).Str("dir", imagesDirectory).Msg("decompressing dump")

			// Decompress the dump

			_, end := profiling.StartTimingCategory(ctx, "compression", utils.Untar)
			err = utils.Untar(path, imagesDirectory)
			end()
			if err != nil {
				return nil, status.Errorf(codes.Internal, "failed to decompress dump: %v", err)
			}
		}

		dir, err = os.Open(imagesDirectory)
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

// This adapter decompresses (if required) the dump to a temporary directory for restore.
// Automatically detects the compression format from the file extension.
func PrepareDumpDirForRestoreVM(next types.RestoreVM) types.RestoreVM {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RestoreVMResp, req *daemon.RestoreVMReq) (chan int, error) {
		path := req.GetVMSnapshotPath()
		stat, err := os.Stat(path)
		if err != nil {
			return nil, status.Errorf(codes.NotFound, "path error: %s", path)
		}

		var dir *os.File
		var imagesDirectory string

		if stat.IsDir() {
			imagesDirectory = path
		} else {
			// Create a temporary directory for the restore
			imagesDirectory = filepath.Join(os.TempDir(), fmt.Sprintf("restore-%d", time.Now().UnixNano()))
			if err := os.Mkdir(imagesDirectory, DUMP_DIR_PERMS); err != nil {
				return nil, status.Errorf(codes.Internal, "failed to create restore dir: %v", err)
			}
			err = os.Chmod(imagesDirectory, DUMP_DIR_PERMS) // XXX: Because for some reason mkdir is not applying perms
			if err != nil {
				return nil, status.Errorf(codes.Internal, "failed to chmod dump dir: %v", err)
			}
			defer os.RemoveAll(imagesDirectory)

			log.Debug().Str("path", path).Str("dir", imagesDirectory).Msg("decompressing dump")

			// Decompress the dump

			_, end := profiling.StartTimingCategory(ctx, "compression", utils.Untar)
			err = utils.Untar(path, imagesDirectory)
			end()
			if err != nil {
				return nil, status.Errorf(codes.Internal, "failed to decompress dump: %v", err)
			}
		}

		dir, err = os.Open(imagesDirectory)
		if err != nil {
			os.RemoveAll(imagesDirectory)
			return nil, status.Errorf(codes.Internal, "failed to open dump dir: %v", err)
		}
		defer dir.Close()

		// Setup dump fs that can be used by future adapters to directly read files
		// to the dump directory
		opts.DumpFs = afero.NewBasePathFs(afero.NewOsFs(), imagesDirectory)

		return next(ctx, opts, resp, req)
	}
}

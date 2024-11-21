package adapters

// Defines all the adapters that manage the file system operations

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"buf.build/gen/go/cedana/daemon/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

const (
	DUMP_DIR_PERMS    = 0o755
	RESTORE_DIR_PERMS = 0o755
)

///////////////////////
//// Dump Adapters ////
///////////////////////

// This adapter ensures the specified dump dir exists and is writable.
// Creates a unique directory within this directory for the dump.
// Updates the CRIU server to use this newly created directory.
// Compresses the dump directory post-dump, based on the compression format provided:
//   - "none" does not compress the dump directory
//   - "tar" creates a tarball of the dump directory
//   - "gzip" creates a gzipped tarball of the dump directory
func PrepareDumpDir(compression string) types.Adapter[types.Dump] {
	return func(next types.Dump) types.Dump {
		return func(ctx context.Context, server types.ServerOpts, resp *daemon.DumpResp, req *daemon.DumpReq) (exited chan int, err error) {
			dir := req.GetDir()

			// Check if the provided dir exists
			if _, err := os.Stat(dir); os.IsNotExist(err) {
				return nil, status.Errorf(codes.InvalidArgument, "dump dir does not exist: %s", dir)
			}

			// Create a unique directory within the dump dir, using type, PID, and timestamp
			imagesDirectory := filepath.Join(dir, fmt.Sprintf("%s-%d",
				req.GetType(),
				time.Now().Unix()))

			// Create the directory
			if err := os.Mkdir(imagesDirectory, DUMP_DIR_PERMS); err != nil {
				return nil, status.Errorf(codes.Internal, "failed to create dump dir: %v", err)
			}

			// Set CRIU server
			f, err := os.Open(imagesDirectory)
			if err != nil {
				os.Remove(imagesDirectory)
				return nil, status.Errorf(codes.Internal, "failed to open dump dir: %v", err)
			}
			defer f.Close()

			if req.GetCriu() == nil {
				req.Criu = &criu.CriuOpts{}
			}

			req.GetCriu().ImagesDir = proto.String(imagesDirectory)
			req.GetCriu().ImagesDirFd = proto.Int32(int32(f.Fd()))

			exited, err = next(ctx, server, resp, req)
			if err != nil {
				os.RemoveAll(imagesDirectory)
				return nil, err
			}

			resp.Path = imagesDirectory

			if compression == "" || compression == "none" {
				return exited, err // Nothing else to do
			}

			// Create the compressed tarball

			var tarball string

			defer os.RemoveAll(imagesDirectory)

			if tarball, err = utils.Tar(imagesDirectory, imagesDirectory, compression); err != nil {
				return exited, status.Errorf(codes.Internal, "failed to create tarball: %v", err)
			}

			resp.Path = tarball

			log.Debug().Str("path", tarball).Str("compression", compression).Msg("created tarball")

			return exited, nil
		}
	}
}

//////////////////////////
//// Restore Adapters ////
//////////////////////////

// This adapter decompresses (if required) the dump to a temporary directory for restore.
// Automatically detects the compression format from the file extension.
func PrepareRestoreDir(next types.Restore) types.Restore {
	return func(ctx context.Context, server types.ServerOpts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (chan int, error) {
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
			imagesDirectory = filepath.Join(os.TempDir(), fmt.Sprintf("restore-%d", time.Now().Unix()))
			if err := os.Mkdir(imagesDirectory, RESTORE_DIR_PERMS); err != nil {
				return nil, status.Errorf(codes.Internal, "failed to create restore dir: %v", err)
			}
			defer os.RemoveAll(imagesDirectory)

			// Decompress the dump
			if err := utils.Untar(path, imagesDirectory); err != nil {
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
			req.Criu = &criu.CriuOpts{}
		}

		req.GetCriu().ImagesDir = proto.String(imagesDirectory)
		req.GetCriu().ImagesDirFd = proto.Int32(int32(dir.Fd()))

		return next(ctx, server, resp, req)
	}
}

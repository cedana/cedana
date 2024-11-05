package adapters

// Defines all the adapters that manage the file system operations

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/cedana/cedana/pkg/api/daemon"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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
// Updates the CRIU opts to use this newly created directory.
// Compresses the dump directory post-dump, based on the compression format provided:
//   - "none" does not compress the dump directory
//   - "tar" creates a tarball of the dump directory
//   - "gzip" creates a gzipped tarball of the dump directory
func PrepareDumpDir(compression string) types.Adapter[types.DumpHandler] {
	return func(h types.DumpHandler) types.DumpHandler {
		return func(ctx context.Context, wg *sync.WaitGroup, resp *daemon.DumpResp, req *daemon.DumpReq) error {
			dir := req.GetDir()

			// Check if the provided dir exists
			if _, err := os.Stat(dir); os.IsNotExist(err) {
				return status.Errorf(codes.InvalidArgument, "dump dir does not exist: %s", dir)
			}

			// Create a unique directory within the dump dir, using type, PID, and timestamp
			imagesDirectory := filepath.Join(dir, fmt.Sprintf("%s-%d",
				req.GetType(),
				time.Now().Unix()))

			// Create the directory
			if err := os.Mkdir(imagesDirectory, DUMP_DIR_PERMS); err != nil {
				return status.Errorf(codes.Internal, "failed to create dump dir: %v", err)
			}

			// Set CRIU opts
			f, err := os.Open(imagesDirectory)
			if err != nil {
				os.Remove(imagesDirectory)
				return status.Errorf(codes.Internal, "failed to open dump dir: %v", err)
			}
			defer f.Close()

			if req.GetCriu() == nil {
				req.Criu = &daemon.CriuOpts{}
			}

			req.GetCriu().ImagesDir = imagesDirectory
			req.GetCriu().ImagesDirFd = int32(f.Fd())

			err = h(ctx, wg, resp, req)
			if err != nil {
				os.RemoveAll(imagesDirectory)
				return err
			}

			resp.Path = imagesDirectory

			if compression == "" || compression == "none" {
				return err // Nothing else to do
			}

			// Create the compressed tarball

			var tarball string

			defer os.RemoveAll(imagesDirectory)

			if tarball, err = utils.Tar(imagesDirectory, imagesDirectory, compression); err != nil {
				return status.Errorf(codes.Internal, "failed to create tarball: %v", err)
			}

			resp.Path = tarball

			log.Debug().Str("path", tarball).Str("compression", compression).Msg("created tarball")

			return nil
		}
	}
}

//////////////////////////
//// Restore Adapters ////
//////////////////////////

// This adapter decompresses (if required) the dump to a temporary directory for restore.
// Automatically detects the compression format from the file extension.
func PrepareRestoreDir(h types.RestoreHandler) types.RestoreHandler {
	return func(ctx context.Context, lifetimeCtx context.Context, wg *sync.WaitGroup, resp *daemon.RestoreResp, req *daemon.RestoreReq) (chan int, error) {
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
			req.Criu = &daemon.CriuOpts{}
		}

		req.GetCriu().ImagesDir = imagesDirectory
		req.GetCriu().ImagesDirFd = int32(dir.Fd())

		return h(ctx, lifetimeCtx, wg, resp, req)
	}
}

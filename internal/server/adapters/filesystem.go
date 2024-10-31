package adapters

// Defines all the adapters that manage the file system operations

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/cedana/cedana/pkg/api/daemon"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	DUMP_DIR_PERMS     = 0755
	DUMP_DIR_FORMATTER = ""
)

// This adapter ensures the specified dump dir exists and is writable.
// Creates a unique directory within this directory for the dump.
// Updates the CRIU opts to use this newly created directory.
// Compresses the dump directory post-dump, based on the compression format provided:
//   - "none" does not compress the dump directory
//   - "tar" creates a tarball of the dump directory
//   - "gzip" creates a gzipped tarball of the dump directory
func PrepareDumpDir(compression string) types.DumpAdapter {
	return func(h types.DumpHandler) types.DumpHandler {
		return func(ctx context.Context, resp *daemon.DumpResp, req *daemon.DumpReq) error {
			dir := req.GetDir()

			// Check if the provided dir exists
			if _, err := os.Stat(dir); os.IsNotExist(err) {
				return status.Errorf(codes.InvalidArgument, "dump dir does not exist: %s", dir)
			}

			// Create a unique directory within the dump dir, using type, PID, and timestamp
			imagesDirectory := filepath.Join(dir, fmt.Sprintf("%s-%d",
				req.GetDetails().GetType(),
				time.Now().Unix()))

			// Create the directory
			if err := os.Mkdir(imagesDirectory, DUMP_DIR_PERMS); err != nil {
				return status.Errorf(codes.Internal, "failed to create dump dir: %v", err)
			}

			// Set CRIU opts
			f, err := os.Open(imagesDirectory)
			if err != nil {
				if os.Remove(imagesDirectory) != nil {
					log.Warn().Err(err).Str("dir", dir).Msg("failed to cleanup dump dir after failure")
				}
				return status.Errorf(codes.Internal, "failed to open dump dir: %v", err)
			}
			defer f.Close()

			if req.GetDetails().GetCriu() == nil {
				req.Details.Criu = &daemon.CriuOpts{}
			}

			req.GetDetails().GetCriu().ImagesDir = imagesDirectory
			req.GetDetails().GetCriu().ImagesDirFd = int32(f.Fd())

			err = h(ctx, resp, req)
			if err != nil {
				os.RemoveAll(imagesDirectory)
				return err
			}

			resp.Path = imagesDirectory

			if compression == "" || compression == "none" {
				return err
			}

			// Create the compressed tarball

			var tarball string

			defer os.RemoveAll(imagesDirectory)
			dumpFiles, err := utils.ListFilesInDir(imagesDirectory)
			if err != nil {
				return status.Errorf(codes.Internal, "failed to list files in dump dir: %v", err)
			}

			if tarball, err = utils.CreateTarball(dumpFiles, imagesDirectory, compression); err != nil {
				return status.Errorf(codes.Internal, "failed to create tarball: %v", err)
			}

			resp.Path = tarball

			log.Debug().Str("path", tarball).Str("compression", compression).Msg("created tarball")

			return nil
		}
	}
}

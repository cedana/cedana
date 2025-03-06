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
	"github.com/cedana/cedana/pkg/utils"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

const DUMP_DIR_PERMS = 0o755

// This adapter ensures the specified dump dir exists and is writable.
// Creates a unique directory within this directory for the dump.
// Updates the CRIU server to use this newly created directory.
// Compresses the dump directory post-dump, based on a compression format:
//   - "none" does not compress the dump directory
//   - "tar" creates a tarball of the dump directory
//   - "gzip" creates a gzipped tarball of the dump directory
//   - "lz4" creates an lz4-compressed tarball of the dump directory
func PrepareDumpDir(next types.Dump) types.Dump {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (exited chan int, err error) {
		compression := req.Compression
		if compression == "" {
			compression = config.Global.Checkpoint.Compression
		}

		// Check if compression is valid, because we don't want to fail after the dump
		// as the process would be killed
		if _, ok := io.SUPPORTED_COMPRESSIONS[compression]; !ok {
			return nil, status.Errorf(codes.Unimplemented, "unsupported compression format '%s'", compression)
		}

		dir := req.GetDir()

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

		if compression == "" || compression == "none" {
			return exited, err // Nothing else to do
		}

		// Create the compressed tarball

		log.Debug().Str("path", imagesDirectory).Str("compression", compression).Msg("creating tarball")

		_, end := profiling.StartTimingCategory(ctx, "compression", utils.Tar)
		tarball, err := utils.Tar(imagesDirectory, imagesDirectory, compression)
		end()
		if err != nil {
			return exited, status.Errorf(codes.Internal, "failed to create tarball: %v", err)
		}

		os.RemoveAll(imagesDirectory)

		resp.Path = tarball

		log.Debug().Str("path", tarball).Str("compression", compression).Msg("created tarball")

		return exited, nil
	}
}

// This adapter ensures the specified dump dir exists and is writable.
// Creates a unique directory within this directory for the dump.
// Updates the CRIU server to use this newly created directory.
// Compresses the dump directory post-dump, based on a compression format:
//   - "none" does not compress the dump directory
//   - "tar" creates a tarball of the dump directory
//   - "gzip" creates a gzipped tarball of the dump directory
//   - "lz4" creates an lz4-compressed tarball of the dump directory
func PrepareDumpVMDir(next types.DumpVM) types.DumpVM {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpVMResp, req *daemon.DumpVMReq) (exited chan int, err error) {
		dir := req.GetDir()
		compression := config.Global.Checkpoint.Compression

		// Check if the provided dir exists
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			return nil, status.Errorf(codes.InvalidArgument, "dump dir does not exist: %s", dir)
		}

		f, err := os.Open(dir)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to open dump dir: %v", err)
		}
		defer f.Close()

		// Setup dump fs that can be used by future adapters to directly read write/extra files
		// to the dump directory
		opts.DumpFs = afero.NewBasePathFs(afero.NewOsFs(), dir)

		exited, err = next(ctx, opts, resp, req)
		if err != nil {
			return nil, err
		}

		// If nothing was put in the directory, remove it and return early
		entries, err := os.ReadDir(dir)
		if err != nil {
			return exited, status.Errorf(codes.Internal, "failed to read dump dir: %v", err)
		}
		if len(entries) == 0 {
			os.RemoveAll(dir)
			return exited, nil
		}

		// Create the compressed tarball
		log.Debug().Str("path", dir).Str("compression", compression).Msg("creating tarball")

		_, end := profiling.StartTimingCategory(ctx, "compression", utils.Tar)
		tarball, err := utils.Tar(dir, dir, compression)
		end()
		if err != nil {
			return exited, status.Errorf(codes.Internal, "failed to create tarball: %v", err)
		}
		os.RemoveAll(dir)

		resp.TarDumpDir = tarball

		log.Debug().Str("path", tarball).Str("compression", compression).Msg("created tarball")

		return exited, nil
	}
}

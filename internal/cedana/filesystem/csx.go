package filesystem

import (
	"context"
	"os"
	"strings"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/io"
	"github.com/cedana/cedana/pkg/profiling"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/spf13/afero"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

func CSXDumpFilesystem(next types.Dump) types.Dump {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (code func() <-chan int, err error) {
		storage := opts.Storage
		dir := req.Dir
		compression := req.Compression

		if !strings.Contains(dir, "csx://") {
			return nil, status.Errorf(codes.Internal, "invalid filesystem selected")
		}

		if compression != "" && compression != "none" {
			return nil, status.Errorf(codes.InvalidArgument, "CSX does not support compression")
		}

		path, cleanup, err := storage.GetPath(ctx, req.Name, io.WRITE_ONLY)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to get path from CSX: %v", err)
		}
		if cleanup != nil {
			defer cleanup()
		}

		// Create the directory
		if err := os.Mkdir(path, DUMP_DIR_PERMS); err != nil {
			return nil, status.Errorf(codes.Internal, "failed to create dump dir: %v", err)
		}

		f, err := os.Open(path)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to open dump dir: %v", err)
		}
		defer f.Close()

		req.Criu.ImagesDir = proto.String(path)
		req.Criu.ImagesDirFd = proto.Int32(int32(f.Fd()))
		opts.DumpFs = afero.NewBasePathFs(afero.NewOsFs(), path)

		resp.Paths = append(resp.Paths, dir+req.Name)
		defer func() {
			size := utils.SizeFromPath(path)
			profiling.AddIO(ctx, size)
		}()

		return next(ctx, opts, resp, req)
	}
}

func CSXRestoreFilesystem(next types.Restore) types.Restore {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (code func() <-chan int, err error) {
		storage := opts.Storage
		path := req.GetPath()

		path, cleanup, err := storage.GetPath(ctx, path, io.READ_ONLY)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to get restore dir: %v", err)
		}
		if cleanup != nil {
			defer cleanup()
		}

		dir, err := os.Open(path)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to open restore dir: %v", err)
		}
		defer dir.Close()

		req.Criu.ImagesDir = proto.String(path)
		req.Criu.ImagesDirFd = proto.Int32(int32(dir.Fd()))

		opts.DumpFs = afero.NewBasePathFs(afero.NewOsFs(), path)

		return next(ctx, opts, resp, req)
	}
}

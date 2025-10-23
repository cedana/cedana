package streamer

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/internal/cedana/filesystem"
	"github.com/cedana/cedana/pkg/config"
	"github.com/cedana/cedana/pkg/io"
	"github.com/cedana/cedana/pkg/plugins"
	"github.com/cedana/cedana/pkg/types"
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

			// Check if compression is valid, because we don't want to fail after the dump
			// as the process would be killed
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
			if err := os.Mkdir(imagesDirectory, filesystem.DUMP_DIR_PERMS); err != nil {
				return nil, status.Errorf(codes.Internal, "failed to create dump dir: %v", err)
			}
			defer func() {
				if err != nil || storage.IsRemote() {
					os.RemoveAll(imagesDirectory)
				}
			}()
			err = os.Chmod(imagesDirectory, filesystem.DUMP_DIR_PERMS) // XXX: Because for some reason mkdir is not applying perms
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

			// Streamer also requires Cedana's CRIU version until the Stream proto option
			// is merged into CRIU upstream.
			if !opts.Plugins.IsInstalled("criu") {
				return nil, status.Errorf(
					codes.FailedPrecondition,
					"Streaming C/R requires the CRIU plugin to be installed. Default CRIU is not supported yet.",
				)
			}

			// Setup dump fs that can be used by future adapters to directly read write/extra files
			// to the dump directory. Here, instead of OsFs we use the streamer's Fs implementation
			// that handles all read/writes directly through streaming.
			var imgStreamer *plugins.Plugin
			if imgStreamer = opts.Plugins.Get("streamer"); !imgStreamer.IsInstalled() {
				return nil, status.Errorf(
					codes.FailedPrecondition,
					"Please install the streamer plugin to use streaming C/R",
				)
			}

			path := req.Dir + "/" + req.Name // do not use filepath.Join as it removes a slash

			var waitForIO func() error
			opts.DumpFs, waitForIO, err = NewStreamingFs(
				ctx,
				imgStreamer.BinaryPaths()[0],
				imagesDirectory,
				storage,
				path,
				streams,
				WRITE_ONLY,
				compression,
			)
			if err != nil {
				return nil, status.Errorf(codes.Internal, "failed to create streaming fs: %v", err)
			}

			// XXX: We do not differentiate between leave-running or not, because unfortunately CRIU
			// does not close the streaming file descriptors on its side when the PostDumpFunc is triggered.
			// This is why the logic here is not the same as that in `filesystem/dump_adapters.go`.

			defer func() {
				err = errors.Join(err, waitForIO())
			}()

			resp.Paths = append(resp.Paths, path)

			return next(ctx, opts, resp, req)
		}
	}
}

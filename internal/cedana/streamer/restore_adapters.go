package streamer

import (
	"context"
	"os"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/pkg/plugins"
	"github.com/cedana/cedana/pkg/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

func RestoreFilesystem(streams int32) types.Adapter[types.Restore] {
	return func(next types.Restore) types.Restore {
		return func(ctx context.Context, opts types.Opts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (code func() <-chan int, err error) {
			storage := opts.Storage
			path := req.GetPath()

			var imagesDirectory string

			if !storage.IsRemote() {
				stat, err := os.Stat(path)
				if err != nil {
					return nil, status.Errorf(codes.NotFound, "path error: %s", path)
				}
				if !stat.IsDir() {
					return nil, status.Errorf(codes.InvalidArgument, "path must be a directory containing streamed images")
				}
				imagesDirectory = path
			} else {
				// For remote storage, we create a temporary directory for CRIU
				imagesDirectory, err = os.MkdirTemp("", "restore-\\*")
				if err != nil {
					return nil, status.Errorf(codes.Internal, "failed to create temp restore dir: %v", err)
				}
				defer os.RemoveAll(imagesDirectory)
			}

			// Streamer also requires Cedana's CRIU version until the Stream proto option
			// is merged into CRIU upstream.
			if !opts.Plugins.IsInstalled("criu") {
				return nil, status.Errorf(
					codes.FailedPrecondition,
					"Streaming C/R requires the CRIU plugin to be installed. Default CRIU is not supported yet.",
				)
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
			req.Criu.Stream = proto.Bool(true)

			// Setup dump fs that can be used by future adapters to directly read write/extra files
			// to the dump directory. Here, instead of OsFs we use the streamer's Fs implementation
			// that handles all read/writes directly through streaming.
			var imgStreamer *plugins.Plugin
			if imgStreamer = opts.Plugins.Get("streamer"); !imgStreamer.IsInstalled() {
				return nil, status.Errorf(
					codes.FailedPrecondition,
					"Provided checkpoint path requires streaming. Please install the streamer plugin to use streaming C/R",
				)
			}

			// Setup filesystem that can be used by future adapters to directly read files from the checkpoint

			var waitForIO func() error
			opts.DumpFs, waitForIO, err = NewStreamingFs(
				ctx,
				imgStreamer.BinaryPaths()[0],
				imagesDirectory,
				storage,
				req.Path,
				streams,
				READ_ONLY,
			)
			if err != nil {
				return nil, status.Errorf(codes.Internal, "failed to create streaming fs: %v", err)
			}

			code, err = next(ctx, opts, resp, req)
			if err != nil {
				return nil, err
			}

			// Wait for all the streaming to finish
			err = waitForIO()
			if err != nil {
				return nil, status.Errorf(codes.Internal, "failed to stream restore: %v", err)
			}

			return code, nil
		}
	}
}

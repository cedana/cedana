package streamer

import (
	"context"
	"os"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/pkg/config"
	"github.com/cedana/cedana/pkg/plugins"
	"github.com/cedana/cedana/pkg/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

func PrepareDumpDirForRestore(next types.Restore) types.Restore {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (exited chan int, err error) {
		path := req.GetPath()
		stat, err := os.Stat(path)
		if err != nil {
			return nil, status.Errorf(codes.NotFound, "path error: %s", path)
		}

		var dir *os.File
		var imagesDirectory string

		if !stat.IsDir() {
			return nil, status.Errorf(codes.InvalidArgument, "path must be a directory container streamed images")
		}

		imagesDirectory = path

		// Streamer also requires Cedana's CRIU version until the Stream proto option
		// is merged into CRIU upstream.
		if !opts.Plugins.IsInstalled("criu") {
			return nil, status.Errorf(
				codes.FailedPrecondition,
				"Streaming C/R requires the CRIU plugin to be installed. Default CRIU is not supported yet.",
			)
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
		req.Criu.Stream = proto.Bool(true)

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

		// Setup dump fs that can be used by future adapters to directly write extra files
		// to the dump directory
		parallelism := req.Stream
		if parallelism == 0 {
			parallelism = config.Global.Checkpoint.Stream
		}
		opts.DumpFs, err = NewStreamingFs(ctx, opts.WG, imgStreamer.BinaryPaths()[0], imagesDirectory, parallelism, READ_ONLY)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to create streaming fs: %v", err)
		}

		return next(ctx, opts, resp, req)
	}
}

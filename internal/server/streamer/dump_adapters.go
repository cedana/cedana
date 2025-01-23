package streamer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/internal/server/filesystem"
	"github.com/cedana/cedana/pkg/config"
	"github.com/cedana/cedana/pkg/plugins"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// TODO: Handle compression in the daemon itself to ensure
// parity with the normal filesystem dump adapter.
var SUPPORTED_STREAMING_COMPRESSION = "lz4"

// This adapter sets up the cedana-image-streamer for directly streaming
// files to the dump directory.
func PrepareDumpDir(next types.Dump) types.Dump {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (exited chan int, err error) {
		compression := req.Compression
		if compression == "" {
			compression = config.Global.Checkpoint.Compression
		}

		// Check if compression is valid, because we don't want to fail after the dump
		// as the process would be killed
		if _, ok := utils.SUPPORTED_COMPRESSIONS[compression]; !ok {
			return nil, status.Errorf(codes.Unimplemented, "unsupported compression format '%s'", compression)
		}

		// Check if compression is valid for streaming, because we don't want to fail after the dump
		// as the process would be killed
		// TODO: Remove this once compression for streaming is handled by daemon
		if compression != SUPPORTED_STREAMING_COMPRESSION {
			resp.Messages = append(resp.Messages,
				fmt.Sprintf("'%s' is not supported for streaming C/R. Using %s instead.", compression, SUPPORTED_STREAMING_COMPRESSION),
			)
		}

		dir := req.GetDir()

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
			if err != nil {
				os.RemoveAll(imagesDirectory)
			}
		}()

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
		parallelism := req.Stream
		if parallelism == 0 {
			parallelism = config.Global.Checkpoint.Stream
		}
		opts.DumpFs, err = NewStreamingFs(ctx, opts.WG, imgStreamer.BinaryPaths()[0], imagesDirectory, parallelism, WRITE_ONLY)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to create streaming fs: %v", err)
		}

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

		log.Debug().Str("path", imagesDirectory).Str("compression", compression).Msg("stream dump completed")

		return exited, nil
	}
}

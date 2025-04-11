package streamer

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/internal/server/filesystem"
	"github.com/cedana/cedana/pkg/config"
	"github.com/cedana/cedana/pkg/io"
	"github.com/cedana/cedana/pkg/plugins"
	"github.com/cedana/cedana/pkg/profiling"
	"github.com/cedana/cedana/pkg/types"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

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
		if err := os.Mkdir(imagesDirectory, filesystem.DUMP_DIR_PERMS); err != nil {
			return nil, status.Errorf(codes.Internal, "failed to create dump dir: %v", err)
		}
		defer func() {
			if err != nil {
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
		parallelism := req.Stream
		if parallelism == 0 {
			parallelism = config.Global.Checkpoint.Stream
		}

		var waitForIO func() error
		var end func()

		streamerCtx, end := profiling.StartTimingCategory(ctx, "streamer", NewStreamingFs)
		opts.DumpFs, waitForIO, err = NewStreamingFs(
			streamerCtx,
			opts.WG,
			imgStreamer.BinaryPaths()[0],
			imagesDirectory,
			parallelism,
			WRITE_ONLY,
			compression,
		)

		end()

		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to create streaming fs: %v", err)
		}

		exited, err = next(ctx, opts, resp, req)
		if err != nil {
			return nil, err
		}

		// Wait for all the streaming to finish
		_, end = profiling.StartTimingCategory(ctx, "streamer", "streamer.WaitForIO")
		err = waitForIO()
		end()
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to stream dump: %v", err)
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

// This adapter sets up the cedana-image-streamer for directly streaming to s3
func PrepareS3Dump(next types.Dump) types.Dump {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (exited chan int, err error) {
		compression := req.Compression
		if compression == "" {
			compression = config.Global.Checkpoint.Compression
		}

		// We don't need a dir for S3 streaming, but useful for FS to create sockets
		// and other files (like dump.log)
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
		err = os.Chmod(imagesDirectory, filesystem.DUMP_DIR_PERMS) // XXX: Because for some reason mkdir is not applying perms
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to chmod dump dir: %v", err)
		}

		f, err := os.Open(imagesDirectory)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to open dump dir: %v", err)
		}
		defer f.Close()

		// Check if compression is valid, because we don't want to fail after the dump
		// as the process would be killed
		if _, ok := io.SUPPORTED_COMPRESSIONS[compression]; !ok {
			return nil, status.Errorf(codes.Unimplemented, "unsupported compression format '%s'", compression)
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

		var waitForIO func() error
		var end func()

		// Building S3 Config
		keyPrefix := req.Details.GetJID()
		if keyPrefix == "" {
			keyPrefix = req.Name // guaranteed for managed jobs
		}

		bucketName := config.Global.S3.BucketName

		s3Config := S3Config{
			BucketName:     bucketName,
			KeyPrefix:      keyPrefix,
			ForcePathStyle: true,
		}

		streamerCtx, end := profiling.StartTimingCategory(ctx, "streamer", NewS3StreamingFs)
		opts.DumpFs, waitForIO, err = NewS3StreamingFs(
			streamerCtx,
			opts.WG,
			imgStreamer.BinaryPaths()[0],
			imagesDirectory,
			parallelism,
			WRITE_ONLY,
			compression,
			s3Config,
		)

		end()
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to create streaming fs: %v", err)
		}

		exited, err = next(ctx, opts, resp, req)
		if err != nil {
			return nil, err
		}

		// Wait for all the streaming to finish
		_, end = profiling.StartTimingCategory(ctx, "streamer", "streamer.WaitForIO")
		err = waitForIO()
		end()
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to stream dump: %v", err)
		}

		resp.Path = strings.Join([]string{"s3:/", s3Config.BucketName, s3Config.KeyPrefix}, "/")

		log.Debug().Str("path", resp.Path).Str("compression", compression).Msg("stream dump completed")

		return exited, nil
	}
}

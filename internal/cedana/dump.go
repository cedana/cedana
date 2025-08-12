package cedana

import (
	"context"
	"fmt"
	"strings"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/cedana/cedana/internal/cedana/criu"
	"github.com/cedana/cedana/internal/cedana/defaults"
	"github.com/cedana/cedana/internal/cedana/filesystem"
	"github.com/cedana/cedana/internal/cedana/gpu"
	"github.com/cedana/cedana/internal/cedana/job"
	"github.com/cedana/cedana/internal/cedana/network"
	"github.com/cedana/cedana/internal/cedana/process"
	"github.com/cedana/cedana/internal/cedana/streamer"
	"github.com/cedana/cedana/internal/cedana/validation"
	"github.com/cedana/cedana/pkg/config"
	"github.com/cedana/cedana/pkg/features"
	"github.com/cedana/cedana/pkg/io"
	"github.com/cedana/cedana/pkg/types"

	"github.com/rs/zerolog/log"
)

func (s *Server) Dump(ctx context.Context, req *daemon.DumpReq) (*daemon.DumpResp, error) {
	// The order below is the order followed before executing
	// the final handler (criu.Dump).

	middleware := types.Middleware[types.Dump]{
		defaults.FillMissingDumpDefaults,
		validation.ValidateDumpRequest,

		pluginDumpStorage,    // detects and plugs in the storage to use
		pluginDumpMiddleware, // middleware from plugins

		// By now we should have the PID
		process.FillProcessStateForDump,
		process.DetectIOUringForDump,
		process.AddExternalFilesForDump,
		// process.CloseCommonFilesForDump,
		network.DetectNetworkOptionsForDump,
		gpu.Dump(s.gpus),

		process.SaveProcessStateForDump,
		criu.CheckOptsForDump,
	}

	dump := criu.Dump.With(middleware...)

	if req.GetDetails().GetJID() != "" { // If using job dump
		dump = dump.With(job.ManageDump(s.jobs))
	}

	opts := types.Opts{
		Lifetime: s.lifetime,
		Plugins:  s.plugins,
		WG:       s.WaitGroup,
	}
	resp := &daemon.DumpResp{}

	criu := criu.New[daemon.DumpReq, daemon.DumpResp](s.plugins)

	_, err := criu(dump)(ctx, opts, resp, req)
	if err != nil {
		log.Error().Err(err).Str("type", req.Type).Msg("dump failed")
		return nil, err
	}

	log.Info().Str("path", resp.Path).Str("type", req.Type).Msg("dump successful")
	resp.Messages = append(resp.Messages, "Dumped to "+resp.Path)

	return resp, nil
}

//////////////////////////
//// Helper Adapters /////
//////////////////////////

// Adapter that inserts new adapters after itself based on the type of dump.
func pluginDumpMiddleware(next types.Dump) types.Dump {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (code func() <-chan int, err error) {
		middleware := types.Middleware[types.Dump]{}
		t := req.GetType()
		switch t {
		case "process":
			middleware = append(
				middleware,
				process.SetPIDForDump,
				process.DetectShellJobForDump,
			)
		default:
			// Insert plugin-specific middleware
			err = features.DumpMiddleware.IfAvailable(func(
				name string,
				pluginMiddleware types.Middleware[types.Dump],
			) error {
				middleware = append(middleware, pluginMiddleware...)
				return nil
			}, t)
			if err != nil {
				return nil, status.Error(codes.Unavailable, err.Error())
			}
		}
		return next.With(middleware...)(ctx, opts, resp, req)
	}
}

// Detects and plugs in the storage to use from the specified path,
// If path is prepended with "plugin://", it will use the plugin storage if
// an available plugin is found and supports the storage feature.
func pluginDumpStorage(next types.Dump) types.Dump {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (code func() <-chan int, err error) {
		dir := req.GetDir()

		var storage io.Storage = &filesystem.Storage{}

		if strings.Contains(dir, "://") {
			pluginName := fmt.Sprintf("storage/%s", strings.Split(dir, "://")[0])
			err := features.Storage.IfAvailable(func(name string, newPluginStorage func(ctx context.Context) (io.Storage, error)) (err error) {
				if newPluginStorage == nil {
					return fmt.Errorf("plugin '%s' does not implement '%s'", name, features.Storage)
				}
				storage, err = newPluginStorage(ctx)
				return err
			}, pluginName)
			if err != nil {
				return nil, status.Error(codes.Unavailable, err.Error())
			}
		}

		opts.Storage = storage

		streams := req.Streams
		if streams == 0 {
			streams = config.Global.Checkpoint.Streams
		}

		filesystem := filesystem.DumpFilesystem
		if streams > 0 {
			filesystem = streamer.DumpFilesystem(streams)
		}

		return next.With(filesystem)(ctx, opts, resp, req)
	}
}

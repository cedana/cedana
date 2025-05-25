package server

import (
	"context"
	"fmt"
	"strings"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/cedana/cedana/internal/server/criu"
	"github.com/cedana/cedana/internal/server/defaults"
	"github.com/cedana/cedana/internal/server/filesystem"
	"github.com/cedana/cedana/internal/server/job"
	"github.com/cedana/cedana/internal/server/network"
	"github.com/cedana/cedana/internal/server/process"
	"github.com/cedana/cedana/internal/server/streamer"
	"github.com/cedana/cedana/internal/server/validation"
	"github.com/cedana/cedana/pkg/config"
	"github.com/cedana/cedana/pkg/features"
	"github.com/cedana/cedana/pkg/io"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/pkg/utils"

	"github.com/rs/zerolog/log"
)

func (s *Server) Dump(ctx context.Context, req *daemon.DumpReq) (*daemon.DumpResp, error) {
	// The order below is the order followed before executing
	// the final handler (criu.Dump).

	storage, err := pluginStorage(ctx, req.Dir)
	if err != nil {
		return nil, err
	}

	setupDumpFS := filesystem.SetupDumpFS(storage)
	if req.Stream > 0 || config.Global.Checkpoint.Stream > 0 {
		setupDumpFS = streamer.SetupDumpFS(storage)
	}

	middleware := types.Middleware[types.Dump]{
		defaults.FillMissingDumpDefaults,
		validation.ValidateDumpRequest,
		setupDumpFS,

		pluginDumpMiddleware, // middleware from plugins

		// Process state-dependent adapters
		process.FillProcessStateForDump,
		process.DetectShellJobForDump,
		process.DetectIOUringForDump,
		process.AddExternalFilesForDump,
		process.CloseCommonFilesForDump,
		network.DetectNetworkOptionsForDump,

		criu.CheckOptsForDump,
	}

	dump := criu.Dump.With(middleware...)

	if req.GetDetails().GetJID() != "" { // If using job dump
		dump = dump.With(job.ManageDump(s.jobs))
	}

	opts := types.Opts{
		Lifetime: s.lifetime,
		Plugins:  s.plugins,
		WG:       s.wg,
	}
	resp := &daemon.DumpResp{}

	criu := criu.New[daemon.DumpReq, daemon.DumpResp](s.plugins)

	_, err = criu(dump)(ctx, opts, resp, req)
	if err != nil {
		return nil, err
	}

	if utils.PathExists(resp.Path) {
		log.Info().Str("path", resp.Path).Str("type", req.Type).Msg("dump successful")
		resp.Messages = append(resp.Messages, "Dumped to "+resp.Path)
	}

	return resp, nil
}

//////////////////////////
//// Helper Adapters /////
//////////////////////////

// Adapter that inserts new adapters after itself based on the type of dump.
func pluginDumpMiddleware(next types.Dump) types.Dump {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (exited chan int, err error) {
		middleware := types.Middleware[types.Dump]{}
		t := req.GetType()
		switch t {
		case "process":
			middleware = append(middleware, process.SetPIDForDump)
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

// Detects and returns the storage to use from the specified path,
// If path is prepended with "plugin://", it will return the plugin storage if
// an available plugin is found and supports the storage feature.
func pluginStorage(ctx context.Context, path string) (io.Storage, error) {
	var storage io.Storage = &filesystem.Storage{}

	if strings.Contains(path, "://") {
		pluginName := fmt.Sprintf("storage/%s", strings.Split(path, "://")[0])
		err := features.Storage.IfAvailable(func(name string, newPluginStorage func(ctx context.Context) (io.Storage, error)) (err error) {
			if pluginStorage == nil {
				return fmt.Errorf("plugin '%s' does not implement '%s'", name, features.Storage)
			}
			storage, err = newPluginStorage(ctx)
			return err
		}, pluginName)
		if err != nil {
			return nil, status.Error(codes.Unavailable, err.Error())
		}
	}

	return storage, nil
}

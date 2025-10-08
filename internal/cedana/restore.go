package cedana

import (
	"context"
	"fmt"
	"strings"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/internal/cedana/criu"
	"github.com/cedana/cedana/internal/cedana/defaults"
	"github.com/cedana/cedana/internal/cedana/filesystem"
	"github.com/cedana/cedana/internal/cedana/gpu"
	"github.com/cedana/cedana/internal/cedana/job"
	"github.com/cedana/cedana/internal/cedana/network"
	"github.com/cedana/cedana/internal/cedana/process"
	"github.com/cedana/cedana/internal/cedana/streamer"
	"github.com/cedana/cedana/internal/cedana/validation"
	"github.com/cedana/cedana/pkg/features"
	"github.com/cedana/cedana/pkg/io"
	"github.com/cedana/cedana/pkg/types"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *Server) Restore(ctx context.Context, req *daemon.RestoreReq) (*daemon.RestoreResp, error) {
	// Add adapters. The order below is the order followed before executing
	// the final handler (criu.Restore).

	middleware := types.Middleware[types.Restore]{
		defaults.FillMissingRestoreDefaults,
		validation.ValidateRestoreRequest,
		process.WritePIDFileForRestore,

		pluginRestoreStorage, // detects and plugs in the storage to use

		process.ReloadProcessStateForRestore,
		network.DetectNetworkOptionsForRestore,
		gpu.Restore(s.gpus),

		pluginRestoreMiddleware, // middleware from plugins

		process.InheritFilesForRestore,
		criu.CheckOptsForRestore,
	}

	restore := criu.Restore.With(middleware...)

	// If using job restore, or restoring as attachable. If restoring as attachable, its necessary
	// that we manage the restore job, as the IO is managed by the daemon.

	if req.GetDetails().GetJID() != "" || req.Attachable {
		restore = restore.With(job.ManageRestore(s.jobs))
	}

	opts := types.Opts{
		Lifetime: s.lifetime,
		Plugins:  s.plugins,
		WG:       s.WaitGroup,
	}
	resp := &daemon.RestoreResp{}

	criu := criu.New[daemon.RestoreReq, daemon.RestoreResp](s.plugins)

	_, err := criu(restore)(ctx, opts, resp, req)
	if err != nil {
		log.Error().Err(err).Str("type", req.Type).Msg("restore failed")
		return nil, err
	}

	log.Info().Uint32("PID", resp.PID).Str("type", req.Type).Msg("restore successful")
	resp.Messages = append(resp.Messages, fmt.Sprintf("Restored successfully, PID: %d", resp.PID))

	return resp, nil
}

// Restore for CedanaRoot struct which avoid the use of jobs and provides runc compatible cli usage
func (s *Cedana) Restore(req *daemon.RestoreReq) (exitCode <-chan int, err error) {
	// Add adapters. The order below is the order followed before executing
	// the final handler (criu.Restore).

	middleware := types.Middleware[types.Restore]{
		defaults.FillMissingRestoreDefaults,
		validation.ValidateRestoreRequest,
		process.WritePIDFileForRestore,

		pluginRestoreStorage, // detects and plugs in the storage to use

		process.ReloadProcessStateForRestore,
		gpu.Restore(s.gpus),

		pluginRestoreMiddleware, // middleware from plugins

		// Process state-dependent adapters
		process.InheritFilesForRestore,
		network.DetectNetworkOptionsForRestore,

		criu.CheckOptsForRestore,
	}

	restore := criu.Restore.With(middleware...)

	opts := types.Opts{
		Lifetime: s.lifetime,
		Plugins:  s.plugins,
		WG:       s.WaitGroup,
	}
	resp := &daemon.RestoreResp{}

	criu := criu.New[daemon.RestoreReq, daemon.RestoreResp](s.plugins)

	code, err := criu(restore)(s.lifetime, opts, resp, req)
	if err != nil {
		log.Error().Err(err).Str("type", req.Type).Msg("restore failed")
		return nil, err
	}

	return code(), nil
}

//////////////////////////
//// Helper Adapters /////
//////////////////////////

// Adapter that inserts new adapters based on the type of restore request
func pluginRestoreMiddleware(next types.Restore) types.Restore {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (code func() <-chan int, err error) {
		middleware := types.Middleware[types.Restore]{}
		t := req.Type
		switch t {
		case "process":
			middleware = append(middleware, process.DetectShellJobForRestore)
		default:
			// Insert plugin-specific middleware
			err = features.RestoreMiddleware.IfAvailable(func(
				name string,
				pluginMiddleware types.Middleware[types.Restore],
			) error {
				middleware = append(middleware, pluginMiddleware...)
				return nil
			}, t)
			if err != nil {
				return nil, status.Error(codes.Unimplemented, err.Error())
			}
		}

		return next.With(middleware...)(ctx, opts, resp, req)
	}
}

// Detects and plugs in the storage to use from the specified path,
// If path is prepended with "plugin://", it will use the plugin storage if
// an available plugin is found and supports the storage feature.
func pluginRestoreStorage(next types.Restore) types.Restore {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (code func() <-chan int, err error) {
		dir := req.GetPath()

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

		streams, err := streamer.IsStreamable(storage, dir)
		if err != nil {
			return nil, status.Error(codes.Internal, fmt.Sprintf("failed to detect restore filesystem to use: %v", err))
		}

		filesystem := filesystem.RestoreFilesystem
		if streams > 0 {
			filesystem = streamer.RestoreFilesystem(streams)
		}

		return next.With(filesystem)(ctx, opts, resp, req)
	}
}

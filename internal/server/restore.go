package server

import (
	"context"
	"fmt"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
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
	"github.com/cedana/cedana/pkg/types"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *Server) Restore(ctx context.Context, req *daemon.RestoreReq) (*daemon.RestoreResp, error) {
	// Add adapters. The order below is the order followed before executing
	// the final handler (criu.Restore).

	storage, err := pluginStorage(req.Path)
	if err != nil {
		return nil, err
	}

	setupRestoreFS := filesystem.SetupRestoreFS(storage)
	if req.Stream > 0 || config.Global.Checkpoint.Stream > 0 {
		setupRestoreFS = streamer.SetupRestoreFS(storage)
	}

	middleware := types.Middleware[types.Restore]{
		defaults.FillMissingRestoreDefaults,
		validation.ValidateRestoreRequest,
		process.WritePIDFileForRestore,
		setupRestoreFS, // auto-detects compression
		process.ReloadProcessStateForRestore,

		pluginRestoreMiddleware, // middleware from plugins

		// Process state-dependent adapters
		process.DetectShellJobForRestore,
		process.InheritFilesForRestore,
		network.DetectNetworkOptionsForRestore,

		criu.CheckOptsForRestore,
	}

	restore := criu.Restore.With(middleware...)

	if req.GetDetails().GetJID() != "" { // If using job restore
		restore = restore.With(job.ManageRestore(s.jobs))
	}

	opts := types.Opts{
		Lifetime: s.lifetime,
		Plugins:  s.plugins,
		WG:       s.wg,
	}
	resp := &daemon.RestoreResp{}

	criu := criu.New[daemon.RestoreReq, daemon.RestoreResp](s.plugins)

	_, err = criu(restore)(ctx, opts, resp, req)
	if err != nil {
		return nil, err
	}

	log.Info().Uint32("PID", resp.PID).Str("type", req.Type).Msg("restore successful")
	resp.Messages = append(resp.Messages, fmt.Sprintf("Restored successfully, PID: %d", resp.PID))

	return resp, nil
}

// Restore for CedanaRoot struct which avoid the use of jobs and provides runc compatible cli usage
func (s *Root) Restore(ctx context.Context, req *daemon.RestoreReq) (*daemon.RestoreResp, error) {
	// Add adapters. The order below is the order followed before executing
	// the final handler (criu.Restore).

	storage, err := pluginStorage(req.Path)
	if err != nil {
		return nil, err
	}

	dumpDirAdapter := filesystem.SetupRestoreFS(storage)
	if req.Stream > 0 || config.Global.Checkpoint.Stream > 0 {
		dumpDirAdapter = streamer.SetupRestoreFS(storage)
	}

	middleware := types.Middleware[types.Restore]{
		defaults.FillMissingRestoreDefaults,
		validation.ValidateRestoreRequest,
		process.WritePIDFileForRestore,
		dumpDirAdapter, // auto-detects compression
		process.ReloadProcessStateForRestore,

		pluginRestoreMiddleware, // middleware from plugins

		// Process state-dependent adapters
		process.DetectShellJobForRestore,
		process.InheritFilesForRestore,
		network.DetectNetworkOptionsForRestore,

		criu.CheckOptsForRestore,
	}

	restore := criu.Restore.With(middleware...)

	opts := types.Opts{
		Lifetime: s.lifetime,
		Plugins:  s.plugins,
		WG:       s.wg,
	}
	resp := &daemon.RestoreResp{}

	criu := criu.New[daemon.RestoreReq, daemon.RestoreResp](s.plugins)

	_, err = criu(restore)(ctx, opts, resp, req)
	if err != nil {
		return nil, err
	}

	log.Info().Uint32("PID", resp.PID).Str("type", req.Type).Msg("restore successful")
	resp.Messages = append(resp.Messages, fmt.Sprintf("Restored successfully, PID: %d", resp.PID))

	return resp, nil
}

//////////////////////////
//// Helper Adapters /////
//////////////////////////

// Adapter that inserts new adapters based on the type of restore request
func pluginRestoreMiddleware(next types.Restore) types.Restore {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (exited chan int, err error) {
		middleware := types.Middleware[types.Restore]{}
		t := req.Type
		switch t {
		case "process":
			// Nothing to do
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

package server

import (
	"context"

	"github.com/cedana/cedana/internal/plugins"
	"github.com/cedana/cedana/internal/server/adapters"
	"github.com/cedana/cedana/internal/server/handlers"
	"github.com/cedana/cedana/pkg/api/daemon"
	"github.com/cedana/cedana/pkg/types"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *Server) Restore(ctx context.Context, req *daemon.RestoreReq) (*daemon.RestoreResp, error) {
	// Add basic adapters. The order below is the order followed before executing
	// the final handler (handlers.CriuRestore). Post-restore, the order is reversed.

	middleware := []types.RestoreAdapter{
		// Bare minimum adapters
		adapters.JobRestoreAdapter,
		fillMissingRestoreDefaults,
		validateRestoreRequest,
		adapters.PrepareRestoreDir, // auto-detects compression

		// Insert type-specific middleware, from plugins or built-in
		insertTypeSpecificRestoreMiddleware,

		// Process state-dependent adapters
		adapters.FillProcessStateForRestore,
		adapters.DetectNetworkOptionsForRestore,
		adapters.DetectShellJobForRestore,
		adapters.InheritOpenFilesForRestore,
	}

	handler := types.AdaptedRestore(handlers.CriuRestore(s.criu), middleware...)

	resp := &daemon.RestoreResp{}
	err := handler(ctx, resp, req)
	if err != nil {
		return nil, err
	}

	log.Info().Uint32("PID", resp.PID).Msg("restore successful")

	return resp, nil
}

//////////////////////////
//// Helper Adapters /////
//////////////////////////

// Adapter that fills missing info from the request using config defaults
func fillMissingRestoreDefaults(h types.RestoreHandler) types.RestoreHandler {
	return func(ctx context.Context, resp *daemon.RestoreResp, req *daemon.RestoreReq) error {
		// Nothing to do, yet

		return h(ctx, resp, req)
	}
}

// Adapter that validates the restore request
func validateRestoreRequest(h types.RestoreHandler) types.RestoreHandler {
	return func(ctx context.Context, resp *daemon.RestoreResp, req *daemon.RestoreReq) error {
		if req.GetPath() == "" {
			return status.Error(codes.InvalidArgument, "no path provided")
		}
		if req.GetDetails() == nil {
			return status.Error(codes.InvalidArgument, "missing details")
		}
		if req.GetDetails().GetType() == "" {
			return status.Error(codes.InvalidArgument, "missing type")
		}

		return h(ctx, resp, req)
	}
}

// Adapter that inserts new adapters based on the type of restore request
func insertTypeSpecificRestoreMiddleware(h types.RestoreHandler) types.RestoreHandler {
	return func(ctx context.Context, resp *daemon.RestoreResp, req *daemon.RestoreReq) error {
		middleware := []types.RestoreAdapter{}
		t := req.GetDetails().GetType()
		switch t {
		case "job":
			return status.Error(codes.InvalidArgument, "please first use JobRestoreAdapter")
		case "process":
			// Nothing to do, yet
		default:
			// Insert plugin-specific middleware
			if p, exists := plugins.LoadedPlugins[t]; exists {
				defer plugins.RecoverFromPanic(t)
				if pluginMiddleware, err := p.Lookup(plugins.FEATURE_DUMP_MIDDLEWARE); err == nil {
					middleware = append(middleware, pluginMiddleware.([]types.RestoreAdapter)...)
				} else {
					return status.Errorf(codes.InvalidArgument, "plugin '%s' has no valid dump middleware: %v", t, err)
				}
			} else {
				return status.Errorf(codes.InvalidArgument, "unknown dump type: %s. maybe a missing plugin?", t)
			}

			return h(ctx, resp, req)
		}
		h = types.AdaptedRestore(h, middleware...)
		return h(ctx, resp, req)
	}
}

package server

import (
	"context"
	"sync"

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

	middleware := []types.Adapter[types.RestoreHandler]{
		// Bare minimum adapters
		adapters.JobRestoreAdapter(s.db),
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

	handler := types.Adapted(handlers.CriuRestore(s.criu), middleware...)

	resp := &daemon.RestoreResp{}

	// s.ctx is the lifetime context of the server, pass it so that
	// managed processes maximum lifetime is the same as the server.
	// It gives adapters the power to control the lifetime of the process. For e.g.,
	// the GPU adapter can use this context to kill the process when GPU support fails.
	_, err := handler(ctx, s.ctx, s.wg, resp, req)
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
	return func(ctx context.Context, lifetimeCtx context.Context, wg *sync.WaitGroup, resp *daemon.RestoreResp, req *daemon.RestoreReq) (chan int, error) {
		// Nothing to do, yet

		return h(ctx, lifetimeCtx, wg, resp, req)
	}
}

// Adapter that validates the restore request
func validateRestoreRequest(h types.RestoreHandler) types.RestoreHandler {
	return func(ctx context.Context, lifetimeCtx context.Context, wg *sync.WaitGroup, resp *daemon.RestoreResp, req *daemon.RestoreReq) (chan int, error) {
		if req.GetPath() == "" {
			return nil, status.Error(codes.InvalidArgument, "no path provided")
		}
		if req.GetType() == "" {
			return nil, status.Error(codes.InvalidArgument, "missing type")
		}

		return h(ctx, lifetimeCtx, wg, resp, req)
	}
}

// Adapter that inserts new adapters based on the type of restore request
func insertTypeSpecificRestoreMiddleware(h types.RestoreHandler) types.RestoreHandler {
	return func(ctx context.Context, lifetimeCtx context.Context, wg *sync.WaitGroup, resp *daemon.RestoreResp, req *daemon.RestoreReq) (chan int, error) {
		middleware := []types.Adapter[types.RestoreHandler]{}
		t := req.GetType()
		switch t {
		case "job":
			return nil, status.Error(codes.InvalidArgument, "please first use JobRestoreAdapter")
		case "process":
			// Nothing to do, yet
		default:
			// Insert plugin-specific middleware
			if p, exists := plugins.LoadedPlugins[t]; exists {
				defer plugins.RecoverFromPanic(t)
				if pluginMiddlewareUntyped, err := p.Lookup(plugins.FEATURE_DUMP_MIDDLEWARE); err == nil {
					pluginMiddleware, ok := pluginMiddlewareUntyped.([]types.Adapter[types.RestoreHandler])
					if !ok {
						return nil, status.Errorf(codes.InvalidArgument, "plugin '%s' has no valid dump middleware: %v", t, err)
					}
					middleware = append(middleware, pluginMiddleware...)
				} else {
					return nil, status.Errorf(codes.InvalidArgument, "plugin '%s' has no valid dump middleware: %v", t, err)
				}
			} else {
				return nil, status.Errorf(codes.InvalidArgument, "unknown dump type: %s. maybe a missing plugin?", t)
			}
		}
		h = types.Adapted(h, middleware...)
		return h(ctx, lifetimeCtx, wg, resp, req)
	}
}

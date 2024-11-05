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

func (s *Server) Start(ctx context.Context, req *daemon.StartReq) (*daemon.StartResp, error) {
	// Add basic adapters. The order below is the order followed before executing
	// the final handler, which depends on the type of job being started, thus it will be
	// inserted from a plugin or will be the built-in process run handler.

	middleware := []types.Adapter[types.StartHandler]{
		// Bare minimum adapters
		adapters.JobStartAdapter(s.db),
		fillMissingStartDefaults,
		validateStartRequest,
	}

	handler := types.Adapted(typeSpecificHandler(), middleware...)

	resp := &daemon.StartResp{}

	// s.ctx is the lifetime context of the server, pass it so that
	// managed processes maximum lifetime is the same as the server.
	// It gives adapters the power to control the lifetime of the process. For e.g.,
	// the GPU adapter can use this context to kill the process when GPU support fails.
	_, err := handler(ctx, s.ctx, s.wg, resp, req)
	if err != nil {
		return nil, err
	}

	log.Info().Str("JID", resp.JID).Uint32("PID", resp.PID).Msg("job started")

	return resp, nil
}

//////////////////////////
//// Helper Adapters /////
//////////////////////////

// Adapter that fills missing info from the request using config defaults
func fillMissingStartDefaults(h types.StartHandler) types.StartHandler {
	return func(ctx context.Context, lifetimeCtx context.Context, wg *sync.WaitGroup, resp *daemon.StartResp, req *daemon.StartReq) (chan int, error) {
		// Nothing to fill in for now
		return h(ctx, lifetimeCtx, wg, resp, req)
	}
}

// Adapter that validates the start request
func validateStartRequest(h types.StartHandler) types.StartHandler {
	return func(ctx context.Context, lifetimeCtx context.Context, wg *sync.WaitGroup, resp *daemon.StartResp, req *daemon.StartReq) (chan int, error) {
		if req.GetType() == "" {
			return nil, status.Errorf(codes.InvalidArgument, "Type is required")
		}
		if req.GetDetails() == nil {
			return nil, status.Errorf(codes.InvalidArgument, "Details are required")
		}
		// Check if JID already exists
		return h(ctx, lifetimeCtx, wg, resp, req)
	}
}

//////////////////////////
//// Helper Handlers /////
//////////////////////////

// Handler that returns the type-specific handler for the job
func typeSpecificHandler() types.StartHandler {
	return func(ctx context.Context, lifetimeCtx context.Context, wg *sync.WaitGroup, resp *daemon.StartResp, req *daemon.StartReq) (chan int, error) {
		t := req.GetType()
		var handler types.StartHandler
		switch t {
		case "job":
			return nil, status.Errorf(codes.InvalidArgument, "please first use JobStartAdapter")
		case "process":
			handler = handlers.Run
		default:
			// Get plugin-specific handler
			if p, exists := plugins.LoadedPlugins[t]; exists {
				defer plugins.RecoverFromPanic(t)
				if pluginHandlerUntyped, err := p.Lookup(plugins.FEATURE_START_HANDLER); err == nil {
					pluginHandler, ok := pluginHandlerUntyped.(types.StartHandler)
					if !ok {
						return nil, status.Errorf(codes.InvalidArgument, "plugin '%s' has no valid start handler: %v", t, err)
					}
					handler = pluginHandler
				} else {
					return nil, status.Errorf(codes.InvalidArgument, "plugin '%s' has no valid start handler: %v", t, err)
				}
			} else {
				return nil, status.Errorf(codes.InvalidArgument, "unknown type: %s. maybe a missing plugin?", t)
			}
		}
		return handler(ctx, lifetimeCtx, wg, resp, req)
	}
}

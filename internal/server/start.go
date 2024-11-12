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

	middleware := types.Middleware[types.StartHandler]{
		// Bare minimum adapters
		adapters.JobStartAdapter(s.db),
		adapters.FillMissingStartDefaults,
		adapters.ValidateStartRequest,
	}

	handler := pluginStartHandler().With(middleware...)

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
//// Helper Handlers /////
//////////////////////////

// Handler that returns the type-specific handler for the job
func pluginStartHandler() types.StartHandler {
	return func(ctx context.Context, lifetimeCtx context.Context, wg *sync.WaitGroup, resp *daemon.StartResp, req *daemon.StartReq) (exited chan int, err error) {
		t := req.GetType()
		var handler types.StartHandler
		switch t {
		case "process":
			handler = handlers.Run
		default:
			// Use plugin-specific handler
			err = plugins.IfFeatureAvailable(plugins.FEATURE_START_HANDLER, func(
				name string,
				pluginHandler types.StartHandler,
			) error {
				handler = pluginHandler
				return nil
			})
			if err != nil {
				return nil, status.Errorf(codes.Unimplemented, err.Error())
			}
		}
		return handler(ctx, lifetimeCtx, wg, resp, req)
	}
}

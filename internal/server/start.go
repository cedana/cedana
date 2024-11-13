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

	middleware := types.Middleware[types.Start]{
		// Bare minimum adapters
		adapters.JobStartAdapter(s.db),
		adapters.FillMissingStartDefaults,
		adapters.ValidateStartRequest,
	}

	start := pluginStartHandler(s.ctx, s.wg).With(middleware...)

	resp := &daemon.StartResp{}

	// s.ctx is the lifetime context of the server, pass it so that
	// managed processes maximum lifetime is the same as the server.
	// It gives adapters the power to control the lifetime of the process. For e.g.,
	// the GPU adapter can use this context to kill the process when GPU support fails.
	_, err := start.Handle(ctx, resp, req)
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
func pluginStartHandler(lifetime context.Context, wg *sync.WaitGroup) types.Handler[types.Start] {
	handler := types.NewHandler[types.Start](wg, nil)
	handler.Lifetime = lifetime
	handler.Handle = func(ctx context.Context, resp *daemon.StartResp, req *daemon.StartReq) (exited chan int, err error) {
		t := req.GetType()
		var handler types.Handler[types.Start]
		switch t {
		case "process":
			handler = handlers.Run(lifetime, wg)
		default:
			// Use plugin-specific handler
			err = plugins.IfFeatureAvailable(plugins.FEATURE_START_HANDLER, func(
				name string,
				pluginHandler types.Handler[types.Start],
			) error {
				handler = pluginHandler
				return nil
			})
			if err != nil {
				return nil, status.Errorf(codes.Unimplemented, err.Error())
			}
		}
		return handler.Handle(ctx, resp, req)
	}
	return handler
}

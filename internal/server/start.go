package server

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/internal/server/adapters"
	"github.com/cedana/cedana/internal/server/handlers"
	"github.com/cedana/cedana/pkg/plugins"
	"github.com/cedana/cedana/pkg/types"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Pluggable features
const featureStartHandler plugins.Feature[types.Start] = "StartHandler"

func (s *Server) Start(ctx context.Context, req *daemon.StartReq) (*daemon.StartResp, error) {
	// Add basic adapters. The order below is the order followed before executing
	// the final handler, which depends on the type of job being started, thus it will be
	// inserted from a plugin or will be the built-in process run handler.

	middleware := types.Middleware[types.Start]{
		// Bare minimum adapters
		adapters.Manage(s.jobs),
		adapters.FillMissingStartDefaults,
		adapters.ValidateStartRequest,
	}

	start := pluginStartHandler().With(middleware...)

	// s.ctx is the lifetime context of the server, pass it so that
	// managed processes maximum lifetime is the same as the server.
	// It gives adapters the power to control the lifetime of the process. For e.g.,
	// the GPU adapter can use this context to kill the process when GPU support fails.
	opts := types.ServerOpts{
		Lifetime: s.lifetime,
		Plugins:  s.plugins,
		WG:       s.wg,
	}
	resp := &daemon.StartResp{}

	_, err := start(ctx, opts, resp, req)
	if err != nil {
		return nil, err
	}

	log.Info().Uint32("PID", resp.PID).Msg("job started")

	return resp, nil
}

//////////////////////////
//// Helper Handlers /////
//////////////////////////

// Handler that returns the type-specific handler for the job
func pluginStartHandler() types.Start {
	return func(ctx context.Context, server types.ServerOpts, resp *daemon.StartResp, req *daemon.StartReq) (exited chan int, err error) {
		t := req.GetType()
		var handler types.Start
		switch t {
		case "process":
			handler = handlers.Run()
		default:
			// Use plugin-specific handler
			err = featureStartHandler.IfAvailable(func(
				name string,
				pluginHandler types.Start,
			) error {
				handler = pluginHandler
				return nil
			})
			if err != nil {
				return nil, status.Errorf(codes.Unimplemented, err.Error())
			}
		}
		return handler(ctx, server, resp, req)
	}
}

package server

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/internal/features"
	"github.com/cedana/cedana/internal/server/defaults"
	"github.com/cedana/cedana/internal/server/job"
	"github.com/cedana/cedana/internal/server/process"
	"github.com/cedana/cedana/internal/server/validation"
	"github.com/cedana/cedana/pkg/types"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *Server) Manage(ctx context.Context, req *daemon.RunReq) (*daemon.RunResp, error) {
	// NOTE: Manage simply reuses the 'run' adapters, but the handler is different
	// and all the handler needs to do is create the illusion that a 'Run' was done.

	// Add adapters. The order below is the order followed before executing
	// the final handler, which depends on the type of job being run, thus it will be
	// inserted from a plugin or will be the built-in process run handler.

	middleware := types.Middleware[types.Run]{
		job.Manage(s.jobs, true),
		defaults.FillMissingRunDefaults,
		validation.ValidateRunRequest,
	}

	run := pluginManageHandler().With(middleware...) // even the handler depends on the type of job

	opts := types.ServerOpts{
		Lifetime: s.lifetime,
		Plugins:  s.plugins,
		WG:       s.wg,
	}

	resp := &daemon.RunResp{}

	_, err := run(ctx, opts, resp, req)
	if err != nil {
		return nil, err
	}

	log.Info().Uint32("PID", resp.PID).Str("type", req.Type).Msg("manage successful")

	return resp, nil
}

//////////////////////////
//// Helper Handlers /////
//////////////////////////

// Handler that returns the type-specific handler for the job
func pluginManageHandler() types.Run {
	return func(ctx context.Context, server types.ServerOpts, resp *daemon.RunResp, req *daemon.RunReq) (exited chan int, err error) {
		t := req.GetType()
		var handler types.Run
		switch t {
		case "process":
			handler = process.Manage
		default:
			// Use plugin-specific handler
			err = features.ManageHandler.IfAvailable(func(name string, pluginHandler types.Run) error {
				handler = pluginHandler
				return nil
			}, t)
			if err != nil {
				return nil, status.Error(codes.Unimplemented, err.Error())
			}
		}
		if req.GPUEnabled {
			log.Warn().Msg("GPU interception must be manually enabled, as it can't be added for already running process/container")
		}
		return handler(ctx, server, resp, req)
	}
}

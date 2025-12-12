package cedana

import (
	"context"
	"fmt"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/internal/cedana/defaults"
	"github.com/cedana/cedana/internal/cedana/gpu"
	"github.com/cedana/cedana/internal/cedana/job"
	"github.com/cedana/cedana/internal/cedana/process"
	"github.com/cedana/cedana/internal/cedana/validation"
	"github.com/cedana/cedana/pkg/features"
	"github.com/cedana/cedana/pkg/profiling"
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
		job.Manage(s.jobs),
		defaults.FillMissingRunDefaults,
		validation.ValidateRunRequest,
		process.WritePIDFile,
		gpu.Attach(s.gpus),

		pluginRunMiddleware, // middleware from plugins
	}

	manage := pluginManageHandler().With(middleware...) // even the handler depends on the type of job

	opts := types.Opts{
		Lifetime: s.lifetime,
		Plugins:  s.plugins,
		WG:       s.WaitGroup,
	}

	resp := &daemon.RunResp{}

	_, err := manage(ctx, opts, resp, req)
	if err != nil {
		return nil, err
	}

	log.Info().Uint32("PID", resp.PID).Str("type", req.Type).Msg("manage successful")
	resp.Messages = append(resp.Messages, fmt.Sprintf("Managing %s PID %d", req.Type, resp.PID))

	return resp, nil
}

//////////////////////////
//// Helper Handlers /////
//////////////////////////

// Handler that returns the type-specific handler for the job
func pluginManageHandler() types.Run {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RunResp, req *daemon.RunReq) (code func() <-chan int, err error) {
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
			var end func()
			ctx, end = profiling.StartTimingCategory(ctx, req.Type, handler)
			defer end()
		}
		if req.GPUEnabled || req.GPUTracing {
			msg := fmt.Sprintf("GPU interception/tracing for %s must be manually enabled.\n"+
				"You may use the `--no-server` run option with `--gpu-enabled` or `--gpu-tracing` to spawn %s", t, t)
			log.Warn().Msgf(msg)
			resp.Messages = append(resp.Messages, msg)
		}
		return handler(ctx, opts, resp, req)
	}
}

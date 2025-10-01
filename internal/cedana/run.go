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
	"github.com/cedana/cedana/pkg/keys"
	"github.com/cedana/cedana/pkg/profiling"
	"github.com/cedana/cedana/pkg/types"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *Server) Run(ctx context.Context, req *daemon.RunReq) (*daemon.RunResp, error) {
	// Add adapters. The order below is the order followed before executing
	// the final handler, which depends on the type of job being run, thus it will be
	// inserted from a plugin or will be the built-in process run handler.

	middleware := types.Middleware[types.Run]{
		job.Manage(s.jobs), // always manage jobs run through daemon
		defaults.FillMissingRunDefaults,
		validation.ValidateRunRequest,
		process.WritePIDFile,
		gpu.Attach(s.gpus),

		pluginRunMiddleware, // middleware from plugins
	}

	run := pluginRunHandler().With(middleware...) // even the handler depends on the type of job

	opts := types.Opts{
		Lifetime: s.lifetime,
		Plugins:  s.plugins,
		WG:       s.WaitGroup,
	}
	resp := &daemon.RunResp{}

	_, err := run(ctx, opts, resp, req)
	if err != nil {
		log.Error().Err(err).Str("type", req.Type).Msg("run failed")
		return nil, err
	}

	log.Info().Uint32("PID", resp.PID).Str("type", req.Type).Msg("run successful")
	resp.Messages = append(resp.Messages, fmt.Sprintf("Running managed %s PID %d", req.Type, resp.PID))

	return resp, nil
}

func (s *Cedana) Run(req *daemon.RunReq) (exitCode <-chan int, err error) {
	// Add adapters. The order below is the order followed before executing
	// the final handler, which depends on the type of job being run, thus it will be
	// inserted from a plugin or will be the built-in process run handler.

	middleware := types.Middleware[types.Run]{
		defaults.FillMissingRunDefaults,
		validation.ValidateRunRequest,
		process.WritePIDFile,
		gpu.Attach(s.gpus),

		pluginRunMiddleware, // middleware from plugins
	}

	run := pluginRunHandler().With(middleware...) // even the handler depends on the type of job

	opts := types.Opts{
		Lifetime: s.lifetime,
		Plugins:  s.plugins,
		WG:       s.WaitGroup,
	}
	resp := &daemon.RunResp{}

	code, err := run(s.lifetime, opts, resp, req)
	if err != nil {
		log.Error().Err(err).Str("type", req.Type).Msg("run failed")
		return nil, err
	}

	return code(), err
}

//////////////////////////
//// Helper Adapters /////
//////////////////////////

// Adapter that inserts new adapters after itself based on the type of run.
func pluginRunMiddleware(next types.Run) types.Run {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RunResp, req *daemon.RunReq) (code func() <-chan int, err error) {
		middleware := types.Middleware[types.Run]{}
		t := req.GetType()
		switch t {
		case "process":
			// Nothing to do
		default:
			// Insert plugin-specific middleware
			err = features.RunMiddleware.IfAvailable(func(
				name string,
				pluginMiddleware types.Middleware[types.Run],
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

//////////////////////////
//// Helper Handlers /////
//////////////////////////

// Handler that returns the type-specific handler for the job
func pluginRunHandler() types.Run {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RunResp, req *daemon.RunReq) (code func() <-chan int, err error) {
		t := req.Type
		var handler types.Run

		daemonless, _ := ctx.Value(keys.DAEMONLESS_CONTEXT_KEY).(bool)

		switch t {
		case "process":
			handler = process.Run
		default:
			// Use plugin-specific handler
			err = features.RunHandler.IfAvailable(func(name string, pluginHandler types.Run) error {
				handler = pluginHandler
				return nil
			}, t)
			if daemonless {
				supported, _ := features.RunDaemonlessSupport.IsAvailable(t)
				if !supported {
					return nil, fmt.Errorf("plugin '%s' does not support daemonless run", t)
				}
			}
			if err != nil {
				return nil, status.Error(codes.Unimplemented, err.Error())
			}

			var end func()
			ctx, end = profiling.StartTimingCategory(ctx, req.Type, handler)
			defer end()

		}
		if req.GPUEnabled {
			handler = handler.With(gpu.Interception)
		}

		return handler(ctx, opts, resp, req)
	}
}

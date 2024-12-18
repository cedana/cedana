package server

import (
	"context"
	"time"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/internal/features"
	"github.com/cedana/cedana/internal/server/defaults"
	"github.com/cedana/cedana/internal/server/gpu"
	"github.com/cedana/cedana/internal/server/job"
	"github.com/cedana/cedana/internal/server/process"
	"github.com/cedana/cedana/internal/server/validation"
	"github.com/cedana/cedana/pkg/config"
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
		job.Manage(s.jobs, false), // always manage jobs run through daemon
		defaults.FillMissingRunDefaults,
		validation.ValidateRunRequest,

		pluginRunMiddleware, // middleware from plugins
	}

	run := pluginRunHandler().With(middleware...) // even the handler depends on the type of job

	var profilingData *daemon.ProfilingData
	if config.Global.Profiling.Enabled {
		profilingData = &daemon.ProfilingData{Name: "run"}
		defer profiling.RecordDuration(time.Now(), profilingData)
	}

	opts := types.ServerOpts{
		Lifetime:  s.lifetime,
		Plugins:   s.plugins,
		WG:        s.wg,
		Profiling: profilingData,
	}
	resp := &daemon.RunResp{Profiling: profilingData}

	_, err := run(ctx, opts, resp, req)
	if err != nil {
		return nil, err
	}

	log.Info().Uint32("PID", resp.PID).Str("type", req.Type).Msg("run successful")

	return resp, nil
}

//////////////////////////
//// Helper Adapters /////
//////////////////////////

// Adapter that inserts new adapters after itself based on the type of run.
func pluginRunMiddleware(next types.Run) types.Run {
	return func(ctx context.Context, server types.ServerOpts, resp *daemon.RunResp, req *daemon.RunReq) (exited chan int, err error) {
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

		return next.With(middleware...)(ctx, server, resp, req)
	}
}

//////////////////////////
//// Helper Handlers /////
//////////////////////////

// Handler that returns the type-specific handler for the job
func pluginRunHandler() types.Run {
	return func(ctx context.Context, server types.ServerOpts, resp *daemon.RunResp, req *daemon.RunReq) (exited chan int, err error) {
		t := req.GetType()
		var handler types.Run
		switch t {
		case "process":
			handler = process.Run
		default:
			// Use plugin-specific handler
			err = features.RunHandler.IfAvailable(func(name string, pluginHandler types.Run) error {
				handler = pluginHandler
				return nil
			}, t)
			if err != nil {
				return nil, status.Error(codes.Unimplemented, err.Error())
			}
		}
		if req.GPUEnabled {
			handler = handler.With(gpu.Interception)
		}
		return handler(ctx, server, resp, req)
	}
}

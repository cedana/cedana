package adapters

// Defines adapters for GPU support. GPU controller attachment is agnostic
// to the job type. GPU interception is specific to the job type. For e.g.,
// for a process, it's simply modifying the environment. For runc, it's
// modifying the config. Therefore:
// NOTE: Each plugin must implement its own support for GPU interception.

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/internal/server/job"
	"github.com/cedana/cedana/pkg/plugins"
	"github.com/cedana/cedana/pkg/types"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Pluggable features
const featureGPUInterceptionMiddleware plugins.Feature[types.Middleware[types.Start]] = "GPUMiddleware"

////////////////////////
//// Start Adapters ////
////////////////////////

// Adapter that adds GPU support to the request.
// GPU Dump/Restore is automatically managed by the job manager using
// CRIU callbacks. Assumes the job is already created (not running).
func AddGPUSupport(jobs job.Manager) types.Adapter[types.Start] {
	return func(next types.Start) types.Start {
		return func(ctx context.Context, server types.ServerOpts, resp *daemon.StartResp, req *daemon.StartReq) (chan int, error) {
			job := jobs.Get(req.JID)
			if job == nil {
				return nil, status.Errorf(codes.NotFound, "job %s not found", req.JID)
			}

			if !server.Plugins.IsInstalled("gpu") {
				return nil, status.Errorf(
					codes.FailedPrecondition,
					"Please install the GPU plugin to use GPU support",
				)
			}

			log.Info().Str("jid", job.JID).Msg("enabling GPU support")

			// Create child lifetime context, so we have cancellation ability over restored
			// process created by the next handler(s).
			lifetime, cancel := context.WithCancel(server.Lifetime)
			server.Lifetime = lifetime

			gpuErr := jobs.AttachGPUAsync(ctx, server.Lifetime, job.JID)

			exited, err := next(ctx, server, resp, req)
			if err != nil {
				cancel()
				<-gpuErr // wait for GPU attach cleanup
				return nil, err
			}

			err = <-gpuErr
			if err != nil {
				cancel()
				return nil, status.Errorf(codes.Internal, "failed to attach GPU: %v", err)
			}

			log.Info().Str("jid", job.JID).Msg("GPU support enabled")

			return exited, nil
		}
	}
}

///////////////////////////////
//// Interception Adapters ////
///////////////////////////////

// Adapter that adds GPU interception to the request based on the job type.
// Each plugin must implement its own support for GPU interception.
func GPUInterception(next types.Start) types.Start {
	return func(ctx context.Context, server types.ServerOpts, resp *daemon.StartResp, req *daemon.StartReq) (chan int, error) {
		t := req.GetType()
		var handler types.Start
		switch t {
		case "process":
			handler = next.With(GPUInterceptionProcess)
		default:
			// Use plugin-specific handler
			err := featureGPUInterceptionMiddleware.IfAvailable(func(
				name string,
				pluginMiddleware types.Middleware[types.Start],
			) error {
				handler = next.With(pluginMiddleware...)
				return nil
			})
			if err != nil {
				return nil, status.Errorf(codes.Unimplemented, err.Error())
			}
		}
		return handler(ctx, server, resp, req)
	}
}

// Adapter that adds GPU interception to a process job.
func GPUInterceptionProcess(next types.Start) types.Start {
	return func(ctx context.Context, server types.ServerOpts, resp *daemon.StartResp, req *daemon.StartReq) (chan int, error) {
		// Check if GPU plugin is installed
		var gpu *plugins.Plugin
		if gpu = server.Plugins.Get("gpu"); gpu.Status != plugins.Installed {
			return nil, status.Errorf(
				codes.FailedPrecondition,
				"Please install the GPU plugin to use GPU support",
			)
		}

		env := req.GetDetails().GetProcessStart().GetEnv()

		env = append(env, "LD_PRELOAD="+gpu.LibraryPaths()[0])
		env = append(env, "CEDANA_JID="+req.JID)

		req.Details.ProcessStart.Env = env

		return next(ctx, server, resp, req)
	}
}

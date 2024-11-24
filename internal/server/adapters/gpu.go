package adapters

import (
	"context"
	"strings"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/internal/server/job"
	"github.com/cedana/cedana/pkg/plugins"
	"github.com/cedana/cedana/pkg/types"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Defines adapters for GPU support

////////////////////////
//// Start Adapters ////
////////////////////////

// Adapter that adds GPU support to the request.
// Assumes the job is already created.
func GPUSupport(jobs job.Manager) types.Adapter[types.Start] {
	return func(next types.Start) types.Start {
		return func(ctx context.Context, server types.ServerOpts, resp *daemon.StartResp, req *daemon.StartReq) (chan int, error) {
			job := jobs.Get(req.JID)
			if job == nil {
				return nil, status.Errorf(codes.NotFound, "job %s not found", req.JID)
			}
			req.JID = job.JID

			// Check if GPU plugin is installed
			var gpu *plugins.Plugin
			if gpu = server.Plugins.Get("gpu"); gpu.Status != plugins.Installed {
				return nil, status.Errorf(
					codes.FailedPrecondition,
					"Please install the GPU plugin to use GPU support",
				)
			}

			log.Info().Str("jid", job.JID).Msg("enabling GPU support...")

			controller := gpu.BinaryPaths()[0]
			interceptor := gpu.LibraryPaths()[0]

			gpuErr := jobs.AttachGPUAsync(ctx, server.WG, job.JID, controller)

			// Create child lifetime context, so we have cancellation ability over restored
			// process created by the next handler(s).
			lifetime, cancel := context.WithCancel(server.Lifetime)
			server.Lifetime = lifetime

			exited, err := next(ctx, server, resp, withGPUInterception(req, interceptor))
			if err != nil {
				cancel()
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

/////////////////
//// Helpers ////
/////////////////

// Adds the GPU interceptor to the LD_PRELOAD env variable of the request.
func withGPUInterception(req *daemon.StartReq, interceptor string) *daemon.StartReq {
	env := req.GetDetails().GetProcessStart().GetEnv()

	// Check if env has existing LD_PRELOAD
	existing := false
	for _, e := range env {
		if strings.HasPrefix(e, "LD_PRELOAD=") {
			// Append the interceptor to the existing LD_PRELOAD
			e = e + ":" + interceptor
			existing = true
			break
		}
	}

	if !existing {
		env = append(env, "LD_PRELOAD="+interceptor)
	}

	env = append(env, "CEDANA_JID="+req.JID)

	return req
}

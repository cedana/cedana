package gpu

// Defines adapters for adding GPU support to a job. GPU controller attachment is agnostic
// to the job type. GPU interception is specific to the job type. For e.g.,
// for a process, it's simply modifying the environment. For runc, it's
// modifying the config. Therefore:
// NOTE: Each plugin must implement its own GPU interception adapter.
// The process one is implmented here.

import (
	"context"
	"syscall"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/features"
	"github.com/cedana/cedana/pkg/plugins"
	"github.com/cedana/cedana/pkg/profiling"
	"github.com/cedana/cedana/pkg/types"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Adapter that adds GPU support to the request.
// GPU Dump/Restore is automatically managed by the job manager using
// CRIU callbacks. Assumes the job is already created (not running).
func Attach(gpus Manager) types.Adapter[types.Run] {
	return func(next types.Run) types.Run {
		return func(ctx context.Context, opts types.Opts, resp *daemon.RunResp, req *daemon.RunReq) (chan int, error) {
			if !opts.Plugins.IsInstalled("gpu") {
				return nil, status.Errorf(codes.FailedPrecondition, "Please install the GPU plugin to use GPU support")
			}

			jid := req.JID
			if jid == "" {
				return nil, status.Errorf(codes.InvalidArgument, "a JID is required for GPU support")
			}

			log.Info().Str("jid", jid).Msg("enabling GPU support")

			// Create child lifetime context, so we have cancellation ability over restored
			// process created by the next handler(s).
			lifetime, cancel := context.WithCancel(opts.Lifetime)
			opts.Lifetime = lifetime

			user := &syscall.Credential{
				Uid:    req.UID,
				Gid:    req.GID,
				Groups: req.Groups,
			}

			env := req.GetEnv()

			pid := make(chan uint32, 1)
			_, end := profiling.StartTimingCategory(ctx, "gpu", gpus.Attach)
			err := gpus.Attach(ctx, lifetime, jid, user, pid, env)
			end()
			if err != nil {
				return nil, status.Errorf(codes.Internal, "failed to attach GPU: %v", err)
			}

			exited, err := next(ctx, opts, resp, req)
			if err != nil {
				cancel()
				return nil, err
			}

			pid <- resp.PID

			log.Info().Str("jid", jid).Msg("GPU support enabled")

			return exited, nil
		}
	}
}

///////////////////////////////
//// Interception Adapters ////
///////////////////////////////

// Adapter that adds GPU interception to the request based on the job type.
// Each plugin must implement its own support for GPU interception.
func Interception(next types.Run) types.Run {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RunResp, req *daemon.RunReq) (chan int, error) {
		t := req.GetType()
		var handler types.Run
		switch t {
		case "process":
			handler = next.With(ProcessInterception)
		default:
			// Use plugin-specific handler
			err := features.GPUInterception.IfAvailable(func(
				name string,
				pluginInterception types.Adapter[types.Run],
			) error {
				handler = next.With(pluginInterception)
				return nil
			}, t)
			if err != nil {
				return nil, status.Errorf(codes.Unimplemented, err.Error())
			}
		}
		return handler(ctx, opts, resp, req)
	}
}

// Adapter that adds GPU interception to a process job.
func ProcessInterception(next types.Run) types.Run {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RunResp, req *daemon.RunReq) (chan int, error) {
		// Check if GPU plugin is installed
		var gpu *plugins.Plugin
		if gpu = opts.Plugins.Get("gpu"); !gpu.IsInstalled() {
			return nil, status.Errorf(
				codes.FailedPrecondition,
				"Please install the GPU plugin to use GPU support",
			)
		}

		env := req.GetEnv()

		env = append(env, "LD_PRELOAD="+gpu.LibraryPaths()[0])
		env = append(env, "CEDANA_JID="+req.JID)

		req.Env = env

		return next(ctx, opts, resp, req)
	}
}

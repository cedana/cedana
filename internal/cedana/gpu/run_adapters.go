package gpu

// Defines adapters for adding GPU support to a job. GPU controller attachment is agnostic
// to the job type. GPU interception/tracing is specific to the job type. For e.g.,
// for a process, it's simply modifying the environment. For runc, it's
// modifying the config.json. Therefore:
// NOTE: Each plugin must implement its own GPU interception adapter.
// The process one is implmented here.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/features"
	"github.com/cedana/cedana/pkg/keys"
	"github.com/cedana/cedana/pkg/profiling"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Adapter that adds GPU support to the request.
func Attach(gpus Manager) types.Adapter[types.Run] {
	return func(next types.Run) types.Run {
		return func(ctx context.Context, opts types.Opts, resp *daemon.RunResp, req *daemon.RunReq) (code func() <-chan int, err error) {
			if !req.GPUEnabled {
				return next(ctx, opts, resp, req)
			}

			if !opts.Plugins.IsInstalled("gpu") {
				return nil, status.Errorf(codes.FailedPrecondition, "Please install the GPU plugin to enable GPU support")
			}

			err = gpus.Sync(ctx)
			if err != nil {
				return nil, status.Errorf(codes.Internal, "failed to sync GPU manager: %v", err)
			}

			pid := make(chan uint32, 1)
			defer close(pid)

			_, end := profiling.StartTimingCategory(ctx, "gpu", gpus.Attach)
			id, err := gpus.Attach(ctx, pid)
			end()
			if err != nil {
				return nil, status.Errorf(codes.Internal, "failed to attach GPU: %v", err)
			}

			ctx = context.WithValue(ctx, keys.GPU_ID_CONTEXT_KEY, id)

			code, err = next(ctx, opts, resp, req)
			if err != nil {
				return nil, err
			}

			pid <- resp.PID

			log.Info().Uint32("PID", resp.PID).Msg("GPU support enabled for process")

			return code, nil
		}
	}
}

///////////////////////////////////////
//// Interception/Tracing Adapters ////
///////////////////////////////////////

// Adapter that adds GPU interception to the request based on the job type.
// Each plugin must implement its own support for GPU interception.
func Interception(next types.Run) types.Run {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RunResp, req *daemon.RunReq) (code func() <-chan int, err error) {
		id, ok := ctx.Value(keys.GPU_ID_CONTEXT_KEY).(string)
		if !ok {
			return nil, status.Errorf(codes.Internal, "failed to get GPU ID from context")
		}

		plugin := opts.Plugins.Get("gpu")
		if !plugin.IsInstalled() {
			return nil, status.Errorf(codes.FailedPrecondition, "Please install the GPU plugin for GPU C/R support")
		}

		logDir, err := EnsureLogDir(id, req.UID, req.GID)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to ensure GPU log dir: %v", err)
		}

		ctx = context.WithValue(ctx, keys.GPU_LOG_DIR_CONTEXT_KEY, logDir)

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
				return nil, status.Error(codes.Unimplemented, err.Error())
			}
		}

		log.Info().Str("plugin", "gpu").Str("ID", id).Str("type", t).Msg("enabling GPU interception")

		return handler(ctx, opts, resp, req)
	}
}

// Adapter that adds GPU tracing to the request based on the job type.
// Each plugin must implement its own support for GPU tracing.
func Tracing(next types.Run) types.Run {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RunResp, req *daemon.RunReq) (code func() <-chan int, err error) {
		id, ok := ctx.Value(keys.GPU_ID_CONTEXT_KEY).(string) // Try to use existing ID
		if !ok {
			id = uuid.New().String()
			ctx = context.WithValue(ctx, keys.GPU_ID_CONTEXT_KEY, id)
		}

		plugin := opts.Plugins.Get("gpu/tracer")
		if !plugin.IsInstalled() {
			return nil, status.Errorf(codes.FailedPrecondition, "Please install the GPU/tracer plugin for GPU tracing support")
		}

		logDir, err := EnsureLogDir(id, req.UID, req.GID)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to ensure GPU log dir: %v", err)
		}

		ctx = context.WithValue(ctx, keys.GPU_LOG_DIR_CONTEXT_KEY, logDir)

		t := req.GetType()
		var handler types.Run
		switch t {
		case "process":
			handler = next.With(ProcessTracing)
		default:
			// Use plugin-specific handler
			err := features.GPUTracing.IfAvailable(func(
				name string,
				pluginTracing types.Adapter[types.Run],
			) error {
				handler = next.With(pluginTracing)
				return nil
			}, t)
			if err != nil {
				return nil, status.Error(codes.Unimplemented, err.Error())
			}
		}

		log.Info().Str("plugin", "gpu/tracer").Str("ID", id).Str("type", t).Msg("enabling GPU tracing")

		return handler(ctx, opts, resp, req)
	}
}

// Adapter that adds GPU interception to a process
func ProcessInterception(next types.Run) types.Run {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RunResp, req *daemon.RunReq) (code func() <-chan int, err error) {
		id, ok := ctx.Value(keys.GPU_ID_CONTEXT_KEY).(string)
		if !ok {
			return nil, status.Errorf(codes.Internal, "failed to get GPU ID from context")
		}
		logDir, ok := ctx.Value(keys.GPU_LOG_DIR_CONTEXT_KEY).(string)
		if !ok {
			return nil, status.Errorf(codes.Internal, "failed to get GPU log dir from context")
		}

		plugin := opts.Plugins.Get("gpu")
		if !plugin.IsInstalled() {
			return nil, status.Errorf(codes.FailedPrecondition, "Please install the GPU plugin for GPU C/R support")
		}

		libSymlink := filepath.Join(logDir, "libcuda.so.1")
		err = os.Symlink(plugin.LibraryPaths()[0], libSymlink)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to create symlink for GPU library: %s", err)
		}

		req.Env = append(req.Env, "NCCL_SHM_DISABLE=1") // Disable for now to avoid conflicts with NCCL's shm usage
		req.Env = append(req.Env, "CEDANA_GPU_ID="+id)
		req.Env = append(req.Env, "CEDANA_GPU_LOG_DIR="+logDir)
		req.Env = append(req.Env, fmt.Sprintf("LD_PRELOAD=%s:%s", libSymlink, utils.Getenv(req.Env, "LD_PRELOAD")))

		return next(ctx, opts, resp, req)
	}
}

// Adapter that adds GPU tracing to a process
func ProcessTracing(next types.Run) types.Run {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RunResp, req *daemon.RunReq) (code func() <-chan int, err error) {
		id, ok := ctx.Value(keys.GPU_ID_CONTEXT_KEY).(string)
		if !ok {
			return nil, status.Errorf(codes.Internal, "failed to get GPU ID from context")
		}
		logDir, ok := ctx.Value(keys.GPU_LOG_DIR_CONTEXT_KEY).(string)
		if !ok {
			return nil, status.Errorf(codes.Internal, "failed to get GPU log dir from context")
		}

		plugin := opts.Plugins.Get("gpu/tracer")
		if !plugin.IsInstalled() {
			return nil, status.Errorf(codes.FailedPrecondition, "Please install the GPU/tracer plugin for GPU tracing support")
		}

		req.Env = append(req.Env, "CEDANA_GPU_ID="+id)
		req.Env = append(req.Env, "CEDANA_GPU_LOG_DIR="+logDir)
		req.Env = append(req.Env, fmt.Sprintf("LD_PRELOAD=%s:%s", plugin.LibraryPaths()[0], utils.Getenv(req.Env, "LD_PRELOAD")))

		return next(ctx, opts, resp, req)
	}
}

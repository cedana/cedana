package gpu

// Defines adapters for adding GPU support to a job. GPU controller attachment is agnostic
// to the job type. GPU interception/tracing is specific to the job type. For e.g.,
// for a process, it's simply modifying the environment. For runc, it's
// modifying the config.json. Therefore:
// NOTE: Each plugin must implement its own GPU interception adapter.
// The process one is implmented here.

import (
	"context"
	"os"
	"path/filepath"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/features"
	"github.com/cedana/cedana/pkg/keys"
	"github.com/cedana/cedana/pkg/profiling"
	"github.com/cedana/cedana/pkg/types"
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
		if !opts.Plugins.Get("gpu").IsInstalled() {
			return nil, status.Errorf(codes.FailedPrecondition, "Please install the GPU plugin for GPU C/R support")
		}

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
		return handler(ctx, opts, resp, req)
	}
}

// Adapter that adds GPU tracing to the request based on the job type.
// Each plugin must implement its own support for GPU tracing.
func Tracing(next types.Run) types.Run {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RunResp, req *daemon.RunReq) (code func() <-chan int, err error) {
		if !opts.Plugins.Get("gpu/tracer").IsInstalled() {
			return nil, status.Errorf(codes.FailedPrecondition, "Please install the GPU tracer plugin to use GPU tracing")
		}

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
		log.Info().Str("type", t).Msg("enabling GPU tracing")
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

		env := req.GetEnv()
		libPath := opts.Plugins.Get("gpu").LibraryPaths()[0]

		// Create a temporary symlink names libcuda.so.1 pointing to libPath
		tmpDir, err := os.MkdirTemp(os.TempDir(), "libcedana-gpu-")
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to create temp directory for GPU library symlink: %s", err)
		}

		err = os.Chown(tmpDir, int(req.GetUID()), int(req.GetGID()))
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to chown temp directory for GPU library symlink: %s", err)
		}

		libSymlink := filepath.Join(tmpDir, "libcuda.so.1")
		err = os.Symlink(libPath, libSymlink)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to create symlink for GPU library: %s", err)
		}

		env = append(env, "LD_PRELOAD="+libSymlink)
		env = append(env, "NCCL_SHM_DISABLE=1") // Disable for now to avoid conflicts with NCCL's shm usage
		env = append(env, "CEDANA_GPU_ID="+id)

		req.Env = env

		return next(ctx, opts, resp, req)
	}
}

// Adapter that adds GPU tracing to a process
func ProcessTracing(next types.Run) types.Run {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RunResp, req *daemon.RunReq) (code func() <-chan int, err error) {
		env := req.GetEnv()
		libPath := opts.Plugins.Get("gpu/tracer").LibraryPaths()[0]

		// Create a temporary symlink names libcuda.so.1 pointing to libPath
		tmpDir, err := os.MkdirTemp(os.TempDir(), "libcedana-gpu-tracer-")
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to create temp directory for GPU tracer library symlink: %s", err)
		}

		err = os.Chown(tmpDir, int(req.GetUID()), int(req.GetGID()))
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to chown temp directory for GPU tracer library symlink: %s", err)
		}

		libSymlink := filepath.Join(tmpDir, "libcuda.so.1")
		err = os.Symlink(libPath, libSymlink)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to create symlink for GPU tracer library: %s", err)
		}

		env = append(env, "LD_PRELOAD="+libSymlink)
		env = append(env, "NCCL_SHM_DISABLE=1") // Disable for now to avoid conflicts with NCCL's shm usage

		req.Env = env

		return next(ctx, opts, resp, req)
	}
}

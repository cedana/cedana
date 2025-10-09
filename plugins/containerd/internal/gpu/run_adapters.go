package gpu

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/keys"
	"github.com/cedana/cedana/pkg/plugins"
	"github.com/cedana/cedana/pkg/types"
	containerd_keys "github.com/cedana/cedana/plugins/containerd/pkg/keys"
	runc_gpu "github.com/cedana/cedana/plugins/runc/pkg/gpu"
	"github.com/containerd/containerd"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Adapter that adds Cedana GPU interception to the container.
// Modifies the spec ephemeraly.
func Interception(next types.Run) types.Run {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RunResp, req *daemon.RunReq) (func() <-chan int, error) {
		container, ok := ctx.Value(containerd_keys.CONTAINER_CONTEXT_KEY).(containerd.Container)
		if !ok {
			return nil, status.Errorf(codes.Internal, "failed to get container from context")
		}
		spec, err := container.Spec(ctx)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to get spec from container: %v", err)
		}

		id, ok := ctx.Value(keys.GPU_ID_CONTEXT_KEY).(string)
		if !ok {
			return nil, status.Errorf(codes.Internal, "failed to get GPU ID from context")
		}

		// Check if GPU plugin is installed
		var gpu *plugins.Plugin
		if gpu = opts.Plugins.Get("gpu"); !gpu.IsInstalled() {
			return nil, status.Errorf(
				codes.FailedPrecondition,
				"Please install the GPU plugin to use GPU support",
			)
		}

		libraryPath := gpu.LibraryPaths()[0]

		err = runc_gpu.AddInterception(spec, libraryPath, id)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to add GPU interception to spec: %v", err)
		}

		err = container.Update(ctx, containerd.UpdateContainerOpts(containerd.WithSpec(spec)))
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to update container with new spec: %v", err)
		}

		return next(ctx, opts, resp, req)
	}
}

// Adapter that adds Cedana GPU tracing to the container.
// Modifies the spec ephemeraly.
func Tracing(next types.Run) types.Run {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RunResp, req *daemon.RunReq) (func() <-chan int, error) {
		container, ok := ctx.Value(containerd_keys.CONTAINER_CONTEXT_KEY).(containerd.Container)
		if !ok {
			return nil, status.Errorf(codes.Internal, "failed to get container from context")
		}
		spec, err := container.Spec(ctx)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to get spec from container: %v", err)
		}

		libraryPath := opts.Plugins.Get("gpu/tracer").LibraryPaths()[0]

		err = runc_gpu.AddTracing(spec, libraryPath)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to add GPU tracing to spec: %v", err)
		}

		err = container.Update(ctx, containerd.UpdateContainerOpts(containerd.WithSpec(spec)))
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to update container with new spec: %v", err)
		}

		return next(ctx, opts, resp, req)
	}
}

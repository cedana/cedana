package gpu

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/types"
	containerd_keys "github.com/cedana/cedana/plugins/containerd/pkg/keys"
	runc_gpu "github.com/cedana/cedana/plugins/runc/pkg/gpu"
	"github.com/containerd/containerd"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Adapter that adds Cedana GPU interception to the container.
// Modifies the spec ephemeraly.
func Interception(id, libPath string, env ...string) types.Adapter[types.Run] {
	return func(next types.Run) types.Run {
		return func(ctx context.Context, opts types.Opts, resp *daemon.RunResp, req *daemon.RunReq) (func() <-chan int, error) {
			container, ok := ctx.Value(containerd_keys.CONTAINER_CONTEXT_KEY).(containerd.Container)
			if !ok {
				return nil, status.Errorf(codes.Internal, "failed to get container from context")
			}
			spec, err := container.Spec(ctx)
			if err != nil {
				return nil, status.Errorf(codes.Internal, "failed to get spec from container: %v", err)
			}

			err = runc_gpu.AddInterception(spec, id, libPath, env...)
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
}

// Adapter that adds Cedana GPU tracing to the container.
// Modifies the spec ephemeraly.
func Tracing(id, libPath string, env ...string) types.Adapter[types.Run] {
	return func(next types.Run) types.Run {
		return func(ctx context.Context, opts types.Opts, resp *daemon.RunResp, req *daemon.RunReq) (func() <-chan int, error) {
			container, ok := ctx.Value(containerd_keys.CONTAINER_CONTEXT_KEY).(containerd.Container)
			if !ok {
				return nil, status.Errorf(codes.Internal, "failed to get container from context")
			}
			spec, err := container.Spec(ctx)
			if err != nil {
				return nil, status.Errorf(codes.Internal, "failed to get spec from container: %v", err)
			}

			err = runc_gpu.AddTracing(spec, id, libPath, env...)
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
}

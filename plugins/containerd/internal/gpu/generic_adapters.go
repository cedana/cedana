package gpu

import (
	"context"

	"github.com/cedana/cedana/pkg/keys"
	"github.com/cedana/cedana/pkg/types"
	containerd_keys "github.com/cedana/cedana/plugins/containerd/pkg/keys"
	"github.com/containerd/containerd"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Adapter that adds Cedana GPU interception to the container.
// Simply adds the env vars for the shim to pick up.
func Interception[REQ, RESP any](next types.Handler[REQ, RESP]) types.Handler[REQ, RESP] {
	return func(ctx context.Context, opts types.Opts, resp *RESP, req *REQ) (func() <-chan int, error) {
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

		spec.Process.Env = append(spec.Process.Env,
			"CEDANA_GPU=1",
			"CEDANA_GPU_ID="+id,
		)

		err = container.Update(ctx, containerd.UpdateContainerOpts(containerd.WithSpec(spec)))
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to update container with new spec: %v", err)
		}

		return next(ctx, opts, resp, req)
	}
}

// Adapter that adds Cedana GPU tracing to the container.
// Simply adds the env vars for the shim to pick up.
func Tracing[REQ, RESP any](next types.Handler[REQ, RESP]) types.Handler[REQ, RESP] {
	return func(ctx context.Context, opts types.Opts, resp *RESP, req *REQ) (func() <-chan int, error) {
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

		spec.Process.Env = append(spec.Process.Env,
			"CEDANA_GPU_TRACING=1",
			"CEDANA_GPU_ID="+id,
		)

		err = container.Update(ctx, containerd.UpdateContainerOpts(containerd.WithSpec(spec)))
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to update container with new spec: %v", err)
		}

		return next(ctx, opts, resp, req)
	}
}

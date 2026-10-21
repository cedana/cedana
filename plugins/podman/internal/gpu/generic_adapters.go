package gpu

import (
	"context"

	"github.com/cedana/cedana/pkg/keys"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/plugins/podman/internal/client"
	podman_keys "github.com/cedana/cedana/plugins/podman/pkg/keys"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Adapter that adds Cedana GPU interception to the container.
// Simply adds the annotations for cedana-oci-runtime to pick up.
func Interception[REQ, RESP any](next types.Handler[REQ, RESP]) types.Handler[REQ, RESP] {
	return func(ctx context.Context, opts types.Opts, resp *RESP, req *REQ) (func() <-chan int, error) {
		createOpts, ok := ctx.Value(podman_keys.CREATE_OPTS_CONTEXT_KEY).(*client.CreateOpts)
		if !ok {
			return nil, status.Errorf(codes.Internal, "failed to get create options from context")
		}
		id, ok := ctx.Value(keys.GPU_ID_CONTEXT_KEY).(string)
		if !ok {
			return nil, status.Errorf(codes.Internal, "failed to get GPU ID from context")
		}

		createOpts.Annotate(podman_keys.GPU_ANNOTATION, "true")
		createOpts.Annotate(podman_keys.GPU_ID_ANNOTATION, id)

		return next(ctx, opts, resp, req)
	}
}

// Adapter that adds Cedana GPU tracing to the container.
// Simply adds the annotations for cedana-oci-runtime to pick up.
func Tracing[REQ, RESP any](next types.Handler[REQ, RESP]) types.Handler[REQ, RESP] {
	return func(ctx context.Context, opts types.Opts, resp *RESP, req *REQ) (func() <-chan int, error) {
		createOpts, ok := ctx.Value(podman_keys.CREATE_OPTS_CONTEXT_KEY).(*client.CreateOpts)
		if !ok {
			return nil, status.Errorf(codes.Internal, "failed to get create options from context")
		}
		id, ok := ctx.Value(keys.GPU_ID_CONTEXT_KEY).(string)
		if !ok {
			return nil, status.Errorf(codes.Internal, "failed to get GPU ID from context")
		}

		createOpts.Annotate(podman_keys.GPU_TRACING_ANNOTATION, "true")
		createOpts.Annotate(podman_keys.GPU_ID_ANNOTATION, id)

		return next(ctx, opts, resp, req)
	}
}

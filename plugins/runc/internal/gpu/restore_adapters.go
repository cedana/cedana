package gpu

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/keys"
	"github.com/cedana/cedana/pkg/plugins"
	"github.com/cedana/cedana/pkg/types"
	runc_gpu "github.com/cedana/cedana/plugins/runc/pkg/gpu"
	runc_keys "github.com/cedana/cedana/plugins/runc/pkg/keys"
	"github.com/opencontainers/runtime-spec/specs-go"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Adapter that restores Cedana GPU interception to the container.
// This is needed as just adding the GPU interception to the container during
// run is not enough. Upon restore, the container is likely being started
// with it's original spec, which does not have the GPU interception.
func RestoreInterceptionIfNeeded(next types.Restore) types.Restore {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (code func() <-chan int, err error) {
		state := resp.GetState()
		if state == nil {
			return nil, status.Errorf(codes.Internal, "state should have been filled by an adapter")
		}
		if !state.GPUEnabled {
			return next(ctx, opts, resp, req)
		}

		spec, ok := ctx.Value(runc_keys.SPEC_CONTEXT_KEY).(*specs.Spec)
		if !ok {
			return nil, status.Errorf(codes.Internal, "failed to get spec from context")
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

		err = runc_gpu.AddGPUInterceptionToSpec(spec, libraryPath, id)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to add GPU interception to spec: %v", err)
		}

		return next(ctx, opts, resp, req)
	}
}

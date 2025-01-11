package gpu

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/plugins"
	"github.com/cedana/cedana/pkg/types"
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
	return func(ctx context.Context, server types.ServerOpts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (chan int, error) {
		state := resp.GetState()
		if state == nil {
			return nil, status.Errorf(codes.Internal, "state should have been filled by an adapter")
		}
		if !state.GPUEnabled {
			return next(ctx, server, resp, req)
		}

		spec, ok := ctx.Value(runc_keys.SPEC_CONTEXT_KEY).(*specs.Spec)
		if !ok {
			return nil, status.Errorf(codes.Internal, "failed to get spec from context")
		}

		jid := req.GetDetails().GetJID()
		if jid == "" {
			return nil, status.Errorf(codes.InvalidArgument, "JID is required for restoring GPU interception.")
		}

		// Check if GPU plugin is installed
		var gpu *plugins.Plugin
		if gpu = server.Plugins.Get("gpu"); gpu.Status != plugins.Installed {
			return nil, status.Errorf(
				codes.FailedPrecondition,
				"Please install the GPU plugin to use GPU support",
			)
		}

		libraryPath := gpu.LibraryPaths()[0]

		err := AddGPUInterceptionToSpec(spec, libraryPath, jid)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to add GPU interception to spec: %v", err)
		}

		return next(ctx, server, resp, req)
	}
}

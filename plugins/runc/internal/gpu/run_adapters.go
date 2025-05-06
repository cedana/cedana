package gpu

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/plugins"
	"github.com/cedana/cedana/pkg/types"
	runc_gpu "github.com/cedana/cedana/plugins/runc/pkg/gpu"
	runc_keys "github.com/cedana/cedana/plugins/runc/pkg/keys"
	"github.com/opencontainers/runtime-spec/specs-go"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Adapter that adds Cedana GPU interception to the container.
// Modifies the spec as necessary.
func Interception(next types.Run) types.Run {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RunResp, req *daemon.RunReq) (chan int, error) {
		spec, ok := ctx.Value(runc_keys.SPEC_CONTEXT_KEY).(*specs.Spec)
		if !ok {
			return nil, status.Errorf(codes.Internal, "failed to get spec from context")
		}

		jid := req.JID
		if jid == "" {
			jid = req.GetDetails().GetRunc().ID
			if jid == "" {
				return nil, status.Errorf(codes.InvalidArgument, "JID is required for GPU interception")
			}
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

		err := runc_gpu.AddGPUInterceptionToSpec(spec, libraryPath, jid)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to add GPU interception to spec: %v", err)
		}

		return next(ctx, opts, resp, req)
	}
}

///////////////////////////
//// Helper Functions ////
///////////////////////////

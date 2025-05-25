package gpu

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Adapter that adds GPU dump to the request.
func Dump(gpus Manager) types.Adapter[types.Dump] {
	return func(next types.Dump) types.Dump {
		return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (chan int, error) {
			if !resp.GetState().GetGPUEnabled() {
				return next(ctx, opts, resp, req)
			}

			if !opts.Plugins.IsInstalled("gpu") {
				return nil, status.Errorf(codes.FailedPrecondition, "Please install the GPU plugin to dump with GPU support")
			}

			pid := resp.GetState().GetPID()

			id, err := gpus.GetID(pid)
			if err != nil {
				return nil, status.Error(codes.Internal, err.Error())
			}

			state := resp.GetState()
			if state == nil {
				return nil, status.Errorf(codes.InvalidArgument, "missing state. it should have been set by the adapter")
			}

			state.GPUID = id

			// Import GPU CRIU callbacks
			opts.CRIUCallback.Include(gpus.CRIUCallback(id, req.Stream))

			return next(ctx, opts, resp, req)
		}
	}
}

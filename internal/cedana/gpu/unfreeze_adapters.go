package gpu

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func Unfreeze(gpus Manager) types.Adapter[types.Freeze] {
	return func(next types.Freeze) types.Freeze {
		return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (code func() <-chan int, err error) {
			state := resp.GetState()
			if state == nil {
				return nil, status.Errorf(
					codes.InvalidArgument,
					"missing state. at least PID is required in resp.state",
				)
			}

			pid := state.GetPID()

			err = gpus.Sync(ctx)
			if err != nil {
				return nil, status.Errorf(codes.Internal, "failed to sync GPU manager: %v", err)
			}

			if !gpus.IsAttached(pid) {
				return next(ctx, opts, resp, req)
			}

			err = gpus.Unfreeze(ctx, pid)
			if err != nil {
				return nil, status.Errorf(codes.Internal, "failed to unfreeze GPU state: %v", err)
			}

			return next(ctx, opts, resp, req)
		}
	}
}

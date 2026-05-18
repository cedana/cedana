package gpu

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/profiling"
	"github.com/cedana/cedana/pkg/types"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func Freeze(gpus Manager) types.Adapter[types.Freeze] {
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

			if !opts.Plugins.IsInstalled("gpu") {
				return nil, status.Errorf(codes.FailedPrecondition, "Please install the GPU plugin to dump with GPU support")
			}

			id := gpus.GetID(pid)

			state.GPUID = id
			state.GPUEnabled = true

			log.Debug().Str("ID", id).Uint32("PID", pid).Msg("GPU freeze starting")

			_, end := profiling.StartTimingCategory(ctx, "gpu", gpus.Freeze)
			err = gpus.Freeze(ctx, pid)
			end()
			if err != nil {
				return nil, status.Errorf(codes.Internal, "failed to freeze GPU state: %v", err)
			}

			log.Info().Str("ID", id).Uint32("PID", pid).Msg("GPU freeze complete")

			return next(ctx, opts, resp, req)
		}
	}
}

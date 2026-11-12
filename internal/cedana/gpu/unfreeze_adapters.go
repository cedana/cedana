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

			if !opts.Plugins.IsInstalled("gpu") {
				return nil, status.Errorf(codes.FailedPrecondition, "Please install the GPU plugin to dump with GPU support")
			}

			id := gpus.GetID(pid)

			state.GPUID = id
			state.GPUEnabled = true

			// Thaw the rest (e.g. the container cgroup) first: the GPU controller
			// is in the container's cgroup, frozen with it, and could not answer the unfreeze.
			// The application then runs before its GPU does, as after a dump that leaves it
			// running: its CUDA calls wait until the GPU is unfrozen.
			code, err = next(ctx, opts, resp, req)
			if err != nil {
				return code, err
			}

			log.Debug().Str("ID", id).Uint32("PID", pid).Msg("GPU unfreeze starting")

			_, end := profiling.StartTimingCategory(ctx, "gpu", gpus.Unfreeze)
			err = gpus.Unfreeze(ctx, pid)
			end()
			if err != nil {
				log.Error().Err(err).Str("ID", id).Uint32("PID", pid).Msg("failed to unfreeze GPU state")
				return code, status.Errorf(codes.Internal, "failed to unfreeze GPU state: %v", err)
			}

			log.Info().Str("ID", id).Uint32("PID", pid).Msg("GPU unfreeze completed")

			return code, nil
		}
	}
}

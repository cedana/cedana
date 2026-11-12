package process

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Unlike dump, unfreeze must go through even if the process is gone, so that
// whatever was frozen along with it (e.g. the container cgroup) gets thawed.
func FillProcessStateForUnfreeze(next types.Unfreeze) types.Unfreeze {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (code func() <-chan int, err error) {
		state := resp.GetState()
		if state == nil {
			return nil, status.Errorf(
				codes.InvalidArgument,
				"missing state. at least PID is required in resp.state",
			)
		}

		if !utils.PidRunning(state.PID) {
			log.Warn().Uint32("PID", state.PID).Msg("process not running, unfreezing whatever remains")
			return next(ctx, opts, resp, req)
		}

		err = utils.FillProcessState(ctx, state.PID, state, true)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to fill process state: %v", err)
		}

		return next(ctx, opts, resp, req)
	}
}

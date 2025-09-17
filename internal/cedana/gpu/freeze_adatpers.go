package gpu

import (
	"context"
	"strings"

	"buf.build/gen/go/cedana/cedana-gpu/protocolbuffers/go/gpu"
	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/config"
	"github.com/cedana/cedana/pkg/types"
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

			var freezeType gpu.FreezeType

			freezeTypeStr := req.GPUFreezeType
			if freezeTypeStr == "" {
				freezeTypeStr = config.Global.GPU.FreezeType
			}

			switch strings.ToUpper(freezeTypeStr) {
			case "IPC":
				freezeType = gpu.FreezeType_FREEZE_TYPE_IPC
			case "NCCL":
				freezeType = gpu.FreezeType_FREEZE_TYPE_NCCL
			}

			err = gpus.Freeze(ctx, pid, freezeType)
			if err != nil {
				return nil, status.Errorf(codes.Internal, "failed to freeze GPU state: %v", err)
			}

			return next(ctx, opts, resp, req)
		}
	}
}

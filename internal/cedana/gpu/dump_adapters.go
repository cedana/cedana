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

// Adapter that adds GPU dump to the request.
func Dump(gpus Manager) types.Adapter[types.Dump] {
	return func(next types.Dump) types.Dump {
		return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (code func() <-chan int, err error) {
			state := resp.GetState()
			if state == nil {
				return nil, status.Errorf(codes.InvalidArgument, "missing state. at least PID is required in resp.state")
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

			// Import GPU CRIU callbacks
			opts.CRIUCallback.Include(gpus.CRIUCallback(id, freezeType))

			return next(ctx, opts, resp, req)
		}
	}
}

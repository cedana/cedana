package adapters

import (
	"context"

	"buf.build/gen/go/cedana/daemon/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Defines adapters for GPU support

func GPUAdapter(next types.Start) types.Start {
	return func(ctx context.Context, server types.ServerOpts, resp *daemon.StartResp, req *daemon.StartReq) (chan int, error) {
		// Attach GPU support to the job

		return nil, status.Error(codes.Unimplemented, "GPU support not implemented")
	}
}

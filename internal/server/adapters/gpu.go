package adapters

import (
	"context"
	"fmt"

	"github.com/cedana/cedana/pkg/api/daemon"
	"github.com/cedana/cedana/pkg/types"
)

// Defines adapters for GPU support

func GPUAdapter(next types.Start) types.Start {
	return func(ctx context.Context, server types.ServerOpts, resp *daemon.StartResp, req *daemon.StartReq) (chan int, error) {
		// Attach GPU support to the job

		return nil, fmt.Errorf("GPU support not implemented")
	}
}

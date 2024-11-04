package adapters

import (
	"context"
	"fmt"
	"sync"

	"github.com/cedana/cedana/pkg/api/daemon"
	"github.com/cedana/cedana/pkg/types"
)

// Defines adapters for GPU support

func GPUAdapter(h types.StartHandler) types.StartHandler {
	return func(ctx context.Context, lifetimeCtx context.Context, wg *sync.WaitGroup, resp *daemon.StartResp, req *daemon.StartReq) (chan int, error) {
		// Attach GPU support to the job
		return nil, fmt.Errorf("GPU support not implemented")
	}
}

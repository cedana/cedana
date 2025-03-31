package defaults

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/config"
	"github.com/cedana/cedana/pkg/types"
)

// Adapter that fills missing info from the request using config defaults
func FillMissingDumpVMDefaults(next types.DumpVM) types.DumpVM {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpVMResp, req *daemon.DumpVMReq) (exited chan int, err error) {
		if req.Dir == "" {
			req.Dir = config.Global.Checkpoint.Dir
		}

		return next(ctx, opts, resp, req)
	}
}

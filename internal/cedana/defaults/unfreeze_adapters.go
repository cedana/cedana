package defaults

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/types"
)

// Adapter that fills missing info from the request using config defaults
func FillMissingUnfreezeDefaults(next types.Dump) types.Dump {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (code func() <-chan int, err error) {
		// Nothing to do yet
		return next(ctx, opts, resp, req)
	}
}

package defaults

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/types"
)

// Adapter that fills missing info from the request using config defaults
func FillMissingRunDefaults(next types.Run) types.Run {
	return func(ctx context.Context, server types.ServerOpts, resp *daemon.RunResp, req *daemon.RunReq) (chan int, error) {
		// Nothing to fill in for now

		return next(ctx, server, resp, req)
	}
}

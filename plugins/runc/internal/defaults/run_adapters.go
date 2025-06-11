package defaults

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/plugins/runc"
	"github.com/cedana/cedana/pkg/types"
)

const DEFAULT_ROOT = "/run/runc"

func FillMissingRunDefaults(next types.Run) types.Run {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RunResp, req *daemon.RunReq) (code func() <-chan int, err error) {
		if req.GetDetails() == nil {
			req.Details = &daemon.Details{}
		}
		if req.GetDetails().GetRunc() == nil {
			req.Details.Runc = &runc.Runc{}
		}
		if req.GetDetails().GetRunc().GetRoot() == "" {
			req.Details.Runc.Root = DEFAULT_ROOT
		}
		if req.GetDetails().GetRunc().GetID() == "" {
			req.Details.Runc.ID = req.JID
		}
		if req.GetDetails().GetRunc().GetRootless() == "" {
			req.Details.Runc.Rootless = "auto"
		}
		return next(ctx, opts, resp, req)
	}
}

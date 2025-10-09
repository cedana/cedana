package defaults

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/gen/plugins/slurm"
	"github.com/cedana/cedana/pkg/types"
)

const DEFAULT_ROOT = "/run/runc"

func FillMissingRunDefaults(next types.Run) types.Run {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RunResp, req *daemon.RunReq) (code func() <-chan int, err error) {
		if req.GetDetails() == nil {
			req.Details = &daemon.Details{}
		}
		if req.GetDetails().GetSlurm() == nil {
			req.Details.Slurm = &slurm.Slurm{}
		}
		if req.GetDetails().GetSlurm().GetID() == "" {
			req.Details.Runc.ID = req.JID
		}

		// daemonless, _ := ctx.Value(keys.DAEMONLESS_CONTEXT_KEY).(bool)
		// if !daemonless {
		// 	req.Details.Runc.NoSubreaper = false // we always reap when the daemon is managing
		// }

		return next(ctx, opts, resp, req)
	}
}

package defaults

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/gen/plugins/slurm"
	"github.com/cedana/cedana/pkg/types"
	"google.golang.org/protobuf/proto"
)

func FillMissingRestoreDefaults(next types.Restore) types.Restore {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (code func() <-chan int, err error) {
		if req.GetDetails() == nil {
			req.Details = &daemon.Details{}
		}
		if req.GetDetails().GetSlurm() == nil {
			req.Details.Slurm = &slurm.Slurm{}
		}
		if req.GetCriu() == nil {
			req.Criu = &criu_proto.CriuOpts{}
		}
		if req.GetDetails().GetSlurm().GetID() == "" {
			req.Details.Slurm.ID = req.GetDetails().GetJID()
		}
		req.Criu.OrphanPtsMaster = proto.Bool(true)

		// daemonless, _ := ctx.Value(keys.DAEMONLESS_CONTEXT_KEY).(bool)
		// if !daemonless {
		// 	req.Details.Runc.NoSubreaper = false // we always reap when we are the daemon is managing
		// }

		return next(ctx, opts, resp, req)
	}
}

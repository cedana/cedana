package defaults

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/plugins/slurm"
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

		req.Criu.Unprivileged = proto.Bool(true)
		req.Criu.ShellJob = proto.Bool(true)
		req.Criu.TcpEstablished = proto.Bool(true)
		if req.GetDetails().GetSlurm().GetID() == "" {
			req.Details.Slurm.ID = req.GetDetails().GetJID()
		}
		req.Criu.OrphanPtsMaster = proto.Bool(true)

		return next(ctx, opts, resp, req)
	}
}

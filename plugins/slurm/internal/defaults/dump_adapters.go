package defaults

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/plugins/slurm"
	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/pkg/types"
	"google.golang.org/protobuf/proto"
)

func FillMissingDumpDefaults(next types.Dump) types.Dump {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (code func() <-chan int, err error) {
		if req.GetDetails() == nil {
			req.Details = &daemon.Details{}
		}
		if req.GetDetails().GetSlurm() == nil {
			req.Details.Slurm = &slurm.Slurm{}
		}
		if req.GetCriu() == nil {
			req.Criu = &criu_proto.CriuOpts{}
		}
		req.Criu.OrphanPtsMaster = proto.Bool(true)

		return next(ctx, opts, resp, req)
	}
}

package defaults

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/plugins/runc"
	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/pkg/types"
	"google.golang.org/protobuf/proto"
)

func FillMissingRestoreDefaults(next types.Restore) types.Restore {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (code func() <-chan int, err error) {
		if req.GetDetails() == nil {
			req.Details = &daemon.Details{}
		}
		if req.GetDetails().GetRunc() == nil {
			req.Details.Runc = &runc.Runc{}
		}
		if req.GetDetails().GetRunc().GetRoot() == "" {
			req.Details.Runc.Root = DEFAULT_ROOT
		}
		if req.GetCriu() == nil {
			req.Criu = &criu_proto.CriuOpts{}
		}
		if req.GetDetails().GetRunc().GetID() == "" {
			req.Details.Runc.ID = req.GetDetails().GetJID()
		}
		if req.GetDetails().GetRunc().GetRootless() == "" {
			req.Details.Runc.Rootless = "auto"
		}
		req.Criu.OrphanPtsMaster = proto.Bool(true)

		return next(ctx, opts, resp, req)
	}
}

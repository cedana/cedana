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
	return func(ctx context.Context, server types.ServerOpts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (chan int, error) {
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
		req.Criu.OrphanPtsMaster = proto.Bool(true)

		return next(ctx, server, resp, req)
	}
}

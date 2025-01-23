package defaults

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/plugins/crio"
	"github.com/cedana/cedana/pkg/types"
)

func FillMissingDumpDefaults(next types.Dump) types.Dump {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (exited chan int, err error) {
		if req.GetDetails() == nil {
			req.Details = &daemon.Details{}
		}
		if req.GetDetails().GetCrio() == nil {
			req.Details.Crio = &crio.Crio{}
		}
		if req.GetDetails().GetCrio().GetContainerStorage() == "" {
			req.Details.Containerd.Address = DEFAULT_STORAGE
		}
		return next(ctx, opts, resp, req)
	}
}

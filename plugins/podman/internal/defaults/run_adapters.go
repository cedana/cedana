package defaults

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/plugins/podman"
	"github.com/cedana/cedana/pkg/types"
)

func FillMissingRunDefaults(next types.Run) types.Run {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RunResp, req *daemon.RunReq) (code func() <-chan int, err error) {
		if req.GetDetails() == nil {
			req.Details = &daemon.Details{}
		}
		if req.GetDetails().GetPodman() == nil {
			req.Details.Podman = &podman.Podman{}
		}
		if req.GetDetails().GetPodman().GetID() == "" {
			req.Details.Podman.ID = req.JID
		}

		return next(ctx, opts, resp, req)
	}
}

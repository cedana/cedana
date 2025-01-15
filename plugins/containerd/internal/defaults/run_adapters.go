package defaults

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/plugins/containerd"
	"github.com/cedana/cedana/pkg/types"
)

const (
	DEFAULT_NAMESPACE = "default"
	BASE_RUNTIME_DIR  = "/run/containerd"
	DEFAULT_ADDRESS   = "/run/containerd/containerd.sock"
)

func FillMissingRunDefaults(next types.Run) types.Run {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RunResp, req *daemon.RunReq) (exited chan int, err error) {
		if req.GetDetails() == nil {
			req.Details = &daemon.Details{}
		}
		if req.GetDetails().GetContainerd() == nil {
			req.Details.Containerd = &containerd.Containerd{}
		}
		if req.GetDetails().GetContainerd().GetAddress() == "" {
			req.Details.Containerd.Address = DEFAULT_ADDRESS
		}
		if req.GetDetails().GetContainerd().GetNamespace() == "" {
			req.Details.Containerd.Namespace = DEFAULT_NAMESPACE
		}
		if req.GetDetails().GetContainerd().GetID() == "" {
			req.Details.Containerd.ID = req.JID
		}

		return next(ctx, opts, resp, req)
	}
}

package defaults

import (
	"context"
	"os"
	"path/filepath"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/plugins/containerd"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/pkg/utils"
)

const DEFAULT_NAMESPACE = "default"

var (
	DEFAULT_ADDRESS  = utils.Getenv(os.Environ(), "CONTAINERD_ADDRESS", "/run/containerd/containerd.sock")
	BASE_RUNTIME_DIR = filepath.Dir(DEFAULT_ADDRESS)
)

func FillMissingRunDefaults(next types.Run) types.Run {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RunResp, req *daemon.RunReq) (code func() <-chan int, err error) {
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

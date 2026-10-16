package defaults

import (
	"context"
	"os"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/plugins/containerd"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/pkg/utils"
)

const (
	DEFAULT_NAMESPACE   = "default"
	DEFAULT_SNAPSHOTTER = "overlayfs"
)

// The runc shim keeps its state under a fixed root, regardless of the address
// containerd listens on (e.g. k3s uses /run/k3s/containerd/containerd.sock but its
// containers are still under /run/containerd/runc/<namespace>)
const BASE_RUNTIME_DIR = "/run/containerd"

var DEFAULT_ADDRESS = utils.Getenv(os.Environ(), "CONTAINERD_ADDRESS", "/run/containerd/containerd.sock")

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
		if req.GetDetails().GetContainerd().GetSnapshotter() == "" {
			req.Details.Containerd.Snapshotter = DEFAULT_SNAPSHOTTER
		}

		return next(ctx, opts, resp, req)
	}
}

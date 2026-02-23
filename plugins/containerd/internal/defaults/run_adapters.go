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

var (
	DEFAULT_ADDRESS  = getDefaultContainerdAddress()
	BASE_RUNTIME_DIR = getDefaultRuntimeDir()
)

func getDefaultRuntimeDir() string {
	// Check env var first
	if dir := utils.Getenv(os.Environ(), "CONTAINERD_RUNTIME_DIR", ""); dir != "" {
		return dir
	}
	// Runc state is always stored under /run/containerd, regardless of
	// where the containerd socket is located (e.g., MicroK8s uses
	// /var/snap/microk8s/common/run/containerd.sock for the socket but
	// /run/containerd/runc for runc state)
	return "/run/containerd"
}

func getDefaultContainerdAddress() string {
	// Check env var first
	if addr := utils.Getenv(os.Environ(), "CONTAINERD_ADDRESS", ""); addr != "" {
		return addr
	}
	// Auto-detect MicroK8s
	if _, err := os.Stat("/var/snap/microk8s"); err == nil {
		return "/var/snap/microk8s/common/run/containerd.sock"
	}
	// Default
	return "/run/containerd/containerd.sock"
}

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

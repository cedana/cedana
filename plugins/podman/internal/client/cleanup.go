package client

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/plugins/podman/pkg/utils"
)

// Implements the cleanup handler for the podman plugin

func Cleanup(ctx context.Context, details *daemon.Details) error {
	_, err := utils.Podman(ctx, "rm", "--force", "--time", "0", details.GetPodman().GetID())
	return err
}

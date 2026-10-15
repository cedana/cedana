package cmd

import (
	"fmt"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/plugins/podman"
	"github.com/cedana/cedana/pkg/keys"
	"github.com/spf13/cobra"
)

var UnfreezeCmd = &cobra.Command{
	Use:   "podman <container>",
	Short: "Unfreeze a podman container",
	Args:  cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		req, ok := cmd.Context().Value(keys.UNFREEZE_REQ_CONTEXT_KEY).(*daemon.DumpReq)
		if !ok {
			return fmt.Errorf("invalid unfreeze request in context")
		}

		var id string
		if len(args) > 0 {
			id = args[0]
		}

		req.Type = "podman"
		req.Details = &daemon.Details{Podman: &podman.Podman{ID: id}}

		return nil
	},
}

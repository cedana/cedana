package cmd

import (
	"fmt"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/plugins/podman"
	"github.com/cedana/cedana/pkg/keys"
	"github.com/spf13/cobra"
)

var ManageCmd = &cobra.Command{
	Use:   "podman <container>",
	Short: "Manage a podman container",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		req, ok := cmd.Context().Value(keys.RUN_REQ_CONTEXT_KEY).(*daemon.RunReq)
		if !ok {
			return fmt.Errorf("invalid run request in context")
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

package cmd

import (
	"context"

	"buf.build/gen/go/cedana/daemon/protocolbuffers/go/daemon"
	"buf.build/gen/go/cedana/daemon/protocolbuffers/go/daemon/plugins/runc"
	"github.com/cedana/cedana/pkg/keys"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/spf13/cobra"
)

var RestoreCmd = &cobra.Command{
	Use:   "runc",
	Short: "Restore a runc container",
	RunE: func(cmd *cobra.Command, args []string) error {
		req := utils.GetContextValSafe(
			cmd.Context(),
			keys.RESTORE_REQ_CONTEXT_KEY,
			&daemon.RestoreReq{},
		)

		req.Type = "runc"
		req.Details = &daemon.Details{Runc: &runc.Details{}}

		ctx := context.WithValue(cmd.Context(), keys.RESTORE_REQ_CONTEXT_KEY, req)
		cmd.SetContext(ctx)

		return nil
	},
}

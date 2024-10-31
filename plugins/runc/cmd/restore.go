package cmd

import (
	"context"

	"github.com/cedana/cedana/pkg/api/daemon"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/spf13/cobra"
)

var RestoreCmd = &cobra.Command{
	Use:   "runc",
	Short: "Restore a runc container",
	RunE: func(cmd *cobra.Command, args []string) error {
		req := utils.GetContextValSafe(cmd.Context(), types.RESTORE_REQ_CONTEXT_KEY, &daemon.RestoreReq{})

		req.Details = &daemon.RestoreDetails{
			Type: "runc",
			Opts: &daemon.RestoreDetails_Runc{},
			Criu: req.GetDetails().GetCriu(),
		}

		ctx := context.WithValue(cmd.Context(), types.RESTORE_REQ_CONTEXT_KEY, req)
		cmd.SetContext(ctx)

		return nil
	},
}

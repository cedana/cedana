package cmd

import (
	"context"

	"github.com/cedana/cedana/pkg/api/daemon"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/spf13/cobra"
)

var DumpCmd = &cobra.Command{
	Use:   "runc",
	Short: "Dump a runc container",
	RunE: func(cmd *cobra.Command, args []string) error {
		req := utils.GetContextValSafe(cmd.Context(), types.DUMP_REQ_CONTEXT_KEY, &daemon.DumpReq{})

		req.Details = &daemon.DumpDetails{
			Type: "runc",
			Opts: &daemon.DumpDetails_Runc{},
			Criu: req.GetDetails().GetCriu(),
		}

		ctx := context.WithValue(cmd.Context(), types.DUMP_REQ_CONTEXT_KEY, req)
		cmd.SetContext(ctx)

		return nil
	},
}

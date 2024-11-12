package cmd

import (
	"context"

	"github.com/cedana/cedana/pkg/api/daemon"
	"github.com/cedana/cedana/pkg/api/plugins/runc"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/pkg/utils"
	runc_types "github.com/cedana/cedana/plugins/runc/pkg/types"
	"github.com/spf13/cobra"
)

func init() {
	DumpCmd.Flags().StringP(runc_types.RootFlag.Full, runc_types.RootFlag.Short, "", "runc root")
}

var DumpCmd = &cobra.Command{
	Use:   "runc <container-id>",
	Short: "Dump a runc container",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		req := utils.GetContextValSafe(cmd.Context(), types.DUMP_REQ_CONTEXT_KEY, &daemon.DumpReq{})

		id := args[0]

		root, _ := cmd.Flags().GetString(runc_types.RootFlag.Full)

		req.Type = "runc"
		req.Details = &daemon.Details{Runc: &runc.Details{
			Root: root,
			ID:   id,
		}}

		ctx := context.WithValue(cmd.Context(), types.DUMP_REQ_CONTEXT_KEY, req)
		cmd.SetContext(ctx)

		return nil
	},
}

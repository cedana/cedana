package cmd

import (
	"context"

	"buf.build/gen/go/cedana/daemon/protocolbuffers/go/daemon"
	"buf.build/gen/go/cedana/daemon/protocolbuffers/go/daemon/plugins/runc"
	"github.com/cedana/cedana/pkg/keys"
	"github.com/cedana/cedana/pkg/utils"
	runc_flags "github.com/cedana/cedana/plugins/runc/pkg/flags"
	"github.com/spf13/cobra"
)

func init() {
	DumpCmd.Flags().StringP(runc_flags.RootFlag.Full, runc_flags.RootFlag.Short, "", "runc root")
}

var DumpCmd = &cobra.Command{
	Use:   "runc <container-id>",
	Short: "Dump a runc container",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		req := utils.GetContextValSafe(cmd.Context(), keys.DUMP_REQ_CONTEXT_KEY, &daemon.DumpReq{})

		id := args[0]

		root, _ := cmd.Flags().GetString(runc_flags.RootFlag.Full)

		req.Type = "runc"
		req.Details = &daemon.Details{Runc: &runc.Details{
			Root: root,
			ID:   id,
		}}

		ctx := context.WithValue(cmd.Context(), keys.DUMP_REQ_CONTEXT_KEY, req)
		cmd.SetContext(ctx)

		return nil
	},
}

package cmd

import (
	"context"
	"fmt"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/plugins/crio"
	"github.com/cedana/cedana/pkg/keys"
	"github.com/cedana/cedana/plugins/crio/pkg/flags"
	"github.com/spf13/cobra"
)

func init() {
	DumpCmd.Flags().StringP(
		flags.ContainerStorage.Full,
		flags.ContainerStorage.Short,
		"",
		"storage location for crio container",
	)
}

var DumpCmd = &cobra.Command{
	Use:   "crio <container-id>",
	Short: "Commit a crio container rootfs",
	Args:  cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		req, ok := cmd.Context().Value(keys.DUMP_REQ_CONTEXT_KEY).(*daemon.DumpReq)
		if !ok {
			return fmt.Errorf("invalid dump request in context")
		}

		var id string
		if len(args) > 0 {
			id = args[0]
		}

		containerStorage, _ := cmd.Flags().GetString(flags.ContainerStorage.Full)
		req.Type = "crio"
		req.Details = &daemon.Details{Crio: crio.Crio{
			ContainerID:      id,
			ContainerStorage: containerStorage,
		}}

		ctx := context.WithValue(cmd.Context(), keys.DUMP_REQ_CONTEXT_KEY, req)
		cmd.SetContext(ctx)

		return nil
	},
}

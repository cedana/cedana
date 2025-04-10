package cmd

import (
	"context"
	"fmt"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/plugins/kata"
	"github.com/cedana/cedana/pkg/keys"
	clh_flags "github.com/cedana/cedana/plugins/cloud-hypervisor/pkg/flags"
	"github.com/spf13/cobra"
)

var DumpCmd = &cobra.Command{
	Use:   "cloud-hypervisor <vm-id>",
	Short: "Dump a clh vm",
	Long:  "Dump a cloud-hypervisor virtual machine",
	Args:  cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		req, ok := cmd.Context().Value(keys.DUMP_REQ_CONTEXT_KEY).(*daemon.DumpVMReq)
		if !ok {
			return fmt.Errorf("invalid dump request in context")
		}

		vmSocket, _ := cmd.Flags().GetString(clh_flags.VmSocketFlag.Full)

		id := args[0]

		req.Details = &daemon.Details{Kata: &kata.Kata{
			VmSocket: vmSocket,
			VmID:     id,
		}}

		ctx := context.WithValue(cmd.Context(), keys.DUMP_REQ_CONTEXT_KEY, req)
		cmd.SetContext(ctx)

		return nil
	},
}

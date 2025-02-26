package cmd

import (
	"context"
	"fmt"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/plugins/kata"
	"github.com/cedana/cedana/pkg/keys"
	kata_flags "github.com/cedana/cedana/plugins/kata/pkg/flags"
	"github.com/spf13/cobra"
)

func init() {
	DumpCmd.Flags().StringP(kata_flags.DirFlag.Full, kata_flags.DirFlag.Short, "", "socket path for full vm snapshot")
	DumpCmd.MarkFlagRequired(kata_flags.DirFlag.Full)

	DumpCmd.Flags().StringP(kata_flags.VmTypeFlag.Full, kata_flags.VmTypeFlag.Short, "cloud-hypervisor", "vm type for full vm snapshot")
	DumpCmd.Flags().Uint32P(kata_flags.PortFlag.Full, kata_flags.PortFlag.Short, 8080, "port for cedana daemon")

	DumpCmd.Flags().StringP(kata_flags.VmSocketFlag.Full, kata_flags.VmSocketFlag.Short, "", "socket path for full vm snapshot")
	DumpCmd.MarkFlagRequired(kata_flags.VmSocketFlag.Full)
}

var DumpCmd = &cobra.Command{
	Use:   "kata",
	Short: "Dump a kata vm or container (w/o rootfs)",
	Long:  "Dump a kata vm or container (w/o rootfs). Provide a vm type for vm snapshots. For container c/r, cedana agent will need to be deployed w/ the running sandbox VM.",
	RunE: func(cmd *cobra.Command, args []string) error {
		req, ok := cmd.Context().Value(keys.DUMP_REQ_CONTEXT_KEY).(*daemon.DumpVMReq)
		if !ok {
			return fmt.Errorf("invalid dump request in context")
		}

		dir, _ := cmd.Flags().GetString(kata_flags.DirFlag.Full)
		port, _ := cmd.Flags().GetUint32(kata_flags.PortFlag.Full)
		vmType, _ := cmd.Flags().GetString(kata_flags.VmTypeFlag.Full)
		vmSocket, _ := cmd.Flags().GetString(kata_flags.VmSocketFlag.Full)
		vmID, _ := cmd.Flags().GetString(kata_flags.VmIDFlag.Full)

		req.Type = vmType
		req.Details = &daemon.Details{Kata: &kata.Kata{
			Dir:      dir,
			Port:     port,
			VmType:   vmType,
			VmSocket: vmSocket,
			VmID:     vmID,
		}}

		ctx := context.WithValue(cmd.Context(), keys.DUMP_REQ_CONTEXT_KEY, req)
		cmd.SetContext(ctx)

		return nil
	},
}

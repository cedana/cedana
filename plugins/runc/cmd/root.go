package cmd

import (
	"fmt"

	runc_flags "github.com/cedana/cedana/plugins/runc/pkg/flags"
	"github.com/spf13/cobra"
)

func init() {
	// Add subcommands
	RootCmd.AddCommand(getIdByNameCmd)

	// Add flags
	getIdByNameCmd.Flags().StringP(runc_flags.RootFlag.Full, runc_flags.RootFlag.Short, "", "runc root")
}

var RootCmd = &cobra.Command{
	Use:   "runc",
	Short: "Runc helper commands",
	Args:  cobra.NoArgs,
}

var getIdByNameCmd = &cobra.Command{
	Use:   "get",
	Short: "Get container id by name",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return fmt.Errorf("not implemented")
	},
}

package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var dockerCmd = &cobra.Command{
	Use:   "docker",
	Short: "Directly checkpoint/restore a container or start a daemon",
	RunE: func(cmd *cobra.Command, args []string) error {
		return fmt.Errorf("error: must also specify dump, restore or daemon")
	},
}

func init() {
	clientCommand.AddCommand(dockerCmd)
}

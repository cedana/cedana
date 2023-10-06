package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Here I am introducing a new command to run the daemon in the background with a grpc server
var clientDaemonRPCCmd = &cobra.Command{
	Use:   "daemon-grpc",
	Short: "Start daemon for cedana client. Must be run as root, needed for all other cedana functionality.",
	RunE: func(cmd *cobra.Command, args []string) error {
		return fmt.Errorf("missing subcommand")
	},
}

func init() {
	rootCmd.AddCommand(clientDaemonRPCCmd)
}

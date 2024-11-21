package cmd

import "github.com/spf13/cobra"

func init() {}

// Parent manage command
var manageCmd = &cobra.Command{
	Use:   "manage",
	Short: "Manage a process/container (create a job)",
	RunE: func(cmd *cobra.Command, args []string) error {
		return nil
	},
}

////////////////////
/// Subcommands  ///
////////////////////

package cmd

import "github.com/spf13/cobra"

var RestoreCmd = &cobra.Command{
	Use:   "runc",
	Short: "Restore a runc container",
	RunE: func(cmd *cobra.Command, args []string) error {
		return nil
	},
}

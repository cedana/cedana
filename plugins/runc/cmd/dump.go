package cmd

import "github.com/spf13/cobra"

var DumpCmd = &cobra.Command{
	Use:   "runc",
	Short: "Dump a runc container",
	RunE: func(cmd *cobra.Command, args []string) error {
		return nil
	},
}

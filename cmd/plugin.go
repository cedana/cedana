package cmd

import "github.com/spf13/cobra"

func init() {
  pluginCmd.AddCommand(pluginListCmd)
  pluginCmd.AddCommand(pluginInstallCmd)
}

var pluginCmd = &cobra.Command{
	Use:   "plugin",
	Short: "Manage plugins",
}

////////////////////
/// Subcommands  ///
////////////////////

var pluginListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all plugins",
	RunE: func(cmd *cobra.Command, args []string) error {
		return nil
	},
}

var pluginInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Install a plugin",
	RunE: func(cmd *cobra.Command, args []string) error {
		return nil
	},
}

package cmd

// By default, implements only the process restore subcommand.
// Other subcommands, for e.g. runc, are imported from installed plugins, as they could
// declare their own flags and subcommands. For runc, see plugins/runc/cmd/restore.go.

import (
	"context"
	"fmt"

	"github.com/cedana/cedana/internal/plugins"
	"github.com/spf13/cobra"
)

func init() {
	restoreCmd.AddCommand(processRestoreCmd)

	// Add commands from plugins
	for _, p := range plugins.LoadedPlugins {
		if pluginRestoreCmd, err := p.Lookup(plugins.FEATURE_RESTORE_CMD); err == nil {
			switch pluginRestoreCmd.(type) {
			case *cobra.Command:
				dumpCmd.AddCommand(pluginRestoreCmd.(*cobra.Command))
			}
		}
	}
}

var restoreCmd = &cobra.Command{
	Use:   "restore",
	Short: "Restore a container/process",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		port, _ := cmd.Flags().GetUint32(portFlag)
		host, _ := cmd.Flags().GetString(hostFlag)
		client, err := NewClient(host, port)
		if err != nil {
			return fmt.Errorf("Error creating client: %v", err)
		}
		ctx := context.WithValue(cmd.Context(), CLIENT_CONTEXT_KEY, client)
		cmd.SetContext(ctx)
		return nil
	},
	PersistentPostRun: func(cmd *cobra.Command, args []string) {
		client := cmd.Context().Value(CLIENT_CONTEXT_KEY).(*Client)
		client.Close()
	},
}

var processRestoreCmd = &cobra.Command{
	Use:   "process",
	Short: "Restore a process",
	RunE: func(cmd *cobra.Command, args []string) error {
		return nil
	},
}

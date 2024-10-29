package cmd

// By default, implements only the process dump subcommand.
// Other subcommands, for e.g. runc, are imported from installed plugins, as they could
// declare their own flags and subcommands. For runc, see plugins/runc/cmd/dump.go.

import (
	"context"
	"fmt"

	"github.com/cedana/cedana/internal/plugins"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

func init() {
	dumpCmd.AddCommand(processDumpCmd)

	// Add commands from supported plugins
	for name, p := range plugins.LoadedPlugins {
		defer plugins.RecoverFromPanic(name)
		if pluginDumpCmd, err := p.Lookup(plugins.FEATURE_DUMP_CMD); err == nil {
			dumpCmd.AddCommand(*pluginDumpCmd.(**cobra.Command))
		} else {
			log.Debug().Str("plugin", name).Err(err).Msg("Plugin does not provide a dump command")
		}
	}
}

var dumpCmd = &cobra.Command{
	Use:   "dump",
	Short: "Dump a container/process",
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

var processDumpCmd = &cobra.Command{
	Use:   "process",
	Short: "Dump a process",
	RunE: func(cmd *cobra.Command, args []string) error {
		return nil
	},
}

package cmd

import (
	"context"
	"fmt"

	"github.com/cedana/cedana/pkg/client"
	"github.com/cedana/cedana/pkg/config"
	"github.com/cedana/cedana/pkg/features"
	"github.com/cedana/cedana/pkg/keys"
	"github.com/spf13/cobra"
)

func init() {
	///////////////////////////////////////////
	// Add subcommands from supported plugins
	///////////////////////////////////////////

	features.QueryCmd.IfAvailable(
		func(name string, pluginCmd *cobra.Command) error {
			queryCmd.AddCommand(pluginCmd)
			return nil
		},
	)
}

var queryCmd = &cobra.Command{
	Use:   "query",
	Short: "Query containers/processes",
	Args:  cobra.ArbitraryArgs,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		client, err := client.New(config.Global.Address, config.Global.Protocol)
		if err != nil {
			return fmt.Errorf("Error creating client: %v", err)
		}

		ctx := context.WithValue(cmd.Context(), keys.CLIENT_CONTEXT_KEY, client)
		cmd.SetContext(ctx)

		return nil
	},

	//******************************************************************************************
	// Let subcommands (incl. from plugins) add details to the request, in the `RunE` hook.
	// And also, call make the request to the server, allowing the plugin to handle it and
	// print the information as it likes.
	//******************************************************************************************

	PersistentPostRunE: func(cmd *cobra.Command, args []string) (err error) {
		client, ok := cmd.Context().Value(keys.CLIENT_CONTEXT_KEY).(*client.Client)
		if !ok {
			return fmt.Errorf("invalid client in context")
		}
		defer client.Close()

		return nil
	},
}

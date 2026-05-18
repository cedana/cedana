package cmd

import (
	"context"
	"fmt"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/client"
	"github.com/cedana/cedana/pkg/config"
	"github.com/cedana/cedana/pkg/features"
	"github.com/cedana/cedana/pkg/flags"
	"github.com/cedana/cedana/pkg/keys"
	"github.com/cedana/cedana/pkg/profiling"
	"github.com/spf13/cobra"
)

func init() {
	// Add common flags
	dumpVMCmd.PersistentFlags().
		StringP(flags.DirFlag.Full, flags.DirFlag.Short, "", "directory to dump into")
	dumpVMCmd.MarkPersistentFlagDirname(flags.DirFlag.Full)

	///////////////////////////////////////////
	// Add subcommands from supported plugins
	///////////////////////////////////////////

	features.DumpVMCmd.IfAvailable(
		func(name string, pluginCmd *cobra.Command) error {
			dumpVMCmd.AddCommand(pluginCmd)

			// TODO: Uncomment below once jobDumpVMCmd is defined

			// Apply all the flags from the plugin command to job subcommand (as optional flags),
			// since the job subcommand can be used to dump any managed entity (even from plugins, like runc),
			// thus it could have specific CLI overrides from plugins.

			// (*pluginCmd).Flags().VisitAll(func(f *pflag.Flag) {
			// 	newFlag := *f
			// 	jobDumpVMCmd.Flags().AddFlag(&newFlag)
			// 	newFlag.Usage = fmt.Sprintf("(%s) %s", name, f.Usage) // Add plugin name to usage
			// })
			return nil
		},
	)
}

var dumpVMCmd = &cobra.Command{
	Use:   "dump-vm",
	Short: "Dump a VM",
	Args:  cobra.ArbitraryArgs,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		dir, _ := cmd.Flags().GetString(flags.DirFlag.Full)

		// Create half-baked request
		req := &daemon.DumpVMReq{Dir: dir}

		ctx := context.WithValue(cmd.Context(), keys.DUMP_REQ_CONTEXT_KEY, req)
		cmd.SetContext(ctx)

		client, err := client.New(config.Global.Address, config.Global.Protocol)
		if err != nil {
			return fmt.Errorf("Error creating client: %v", err)
		}

		ctx = context.WithValue(ctx, keys.CLIENT_CONTEXT_KEY, client)
		cmd.SetContext(ctx)

		return nil
	},

	//******************************************************************************************
	// Let subcommands (incl. from plugins) add details to the request, in the `RunE` hook.
	// Finally, we send the request to the server in the PersistentPostRun hook.
	// The server will make sure to handle it appropriately using any required plugins.
	//******************************************************************************************

	PersistentPostRunE: func(cmd *cobra.Command, args []string) (err error) {
		client, ok := cmd.Context().Value(keys.CLIENT_CONTEXT_KEY).(*client.Client)
		if !ok {
			return fmt.Errorf("invalid client in context")
		}
		defer client.Close()

		// Assuming request is now ready to be sent to the server
		req, ok := cmd.Context().Value(keys.DUMP_REQ_CONTEXT_KEY).(*daemon.DumpVMReq)
		if !ok {
			return fmt.Errorf("invalid request in context")
		}

		_, data, err := client.DumpVM(cmd.Context(), req)
		if err != nil {
			return err
		}

		if config.Global.Profiling.Enabled && data != nil {
			profiling.Print(data, features.Theme())
		}

		return nil
	},
}

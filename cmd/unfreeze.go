package cmd

import (
	"context"
	"fmt"
	"strconv"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/client"
	"github.com/cedana/cedana/pkg/config"
	"github.com/cedana/cedana/pkg/features"
	"github.com/cedana/cedana/pkg/keys"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"google.golang.org/protobuf/proto"
)

// By default, implements only the process unfreeze subcommand.
// Other subcommands, for e.g. runc, are imported from installed plugins, as they could
// declare their own flags and subcommands. For runc, see plugins/runc/cmd/unfreeze.go.

func init() {
	unfreezeCmd.AddCommand(processUnfreezeCmd)
	unfreezeCmd.AddCommand(jobUnfreezeCmd)

	///////////////////////////////////////////
	// Add subcommands from supported plugins
	///////////////////////////////////////////

	features.UnfreezeCmd.IfAvailable(
		func(name string, pluginCmd *cobra.Command) error {
			unfreezeCmd.AddCommand(pluginCmd)

			// Apply all the flags from the plugin command to job subcommand (as optional flags),
			// since the job subcommand can be used to unfreeze any managed entity (even from plugins, like runc),
			// thus it could have specific CLI overrides from plugins.

			(*pluginCmd).Flags().VisitAll(func(f *pflag.Flag) {
				newFlag := *f
				jobUnfreezeCmd.Flags().AddFlag(&newFlag)
				newFlag.Usage = fmt.Sprintf("(%s) %s", name, f.Usage) // Add plugin name to usage
			})
			return nil
		},
	)
}

var unfreezeCmd = &cobra.Command{
	Use:   "unfreeze",
	Short: "Unfreeze a container/process",
	Args:  cobra.ArbitraryArgs,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// Create half-baked request
		req := &daemon.DumpReq{
			Action: daemon.DumpAction_UNFREEZE_ONLY,
		}

		ctx := context.WithValue(cmd.Context(), keys.UNFREEZE_REQ_CONTEXT_KEY, req)
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
		req, ok := cmd.Context().Value(keys.UNFREEZE_REQ_CONTEXT_KEY).(*daemon.DumpReq)
		if !ok {
			return fmt.Errorf("invalid request in context")
		}

		resp, profiling, err := client.Unfreeze(cmd.Context(), req)
		if err != nil {
			return err
		}

		if config.Global.Profiling.Enabled && profiling != nil {
			printProfilingData(profiling)
		}

		for _, message := range resp.GetMessages() {
			fmt.Println(message)
		}

		return nil
	},
}

////////////////////
/// Subcommands  ///
////////////////////

var processUnfreezeCmd = &cobra.Command{
	Use:   "process <PID>",
	Short: "Unfreeze a process",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// All we need to do is modify the request to include the PID of the process to dump.
		// And modify the request type.
		req, ok := cmd.Context().Value(keys.UNFREEZE_REQ_CONTEXT_KEY).(*daemon.DumpReq)
		if !ok {
			return fmt.Errorf("invalid request in context")
		}

		pid, err := strconv.ParseUint(args[0], 10, 32)
		if err != nil {
			return fmt.Errorf("invalid pid: %v", err)
		}

		req.Type = "process"
		req.Details = &daemon.Details{Process: &daemon.Process{PID: uint32(pid)}}

		ctx := context.WithValue(cmd.Context(), keys.UNFREEZE_REQ_CONTEXT_KEY, req)
		cmd.SetContext(ctx)

		return nil
	},
}

var jobUnfreezeCmd = &cobra.Command{
	Use:               "job <JID>",
	Short:             "Unfreeze a managed process/container (job)",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: RunningJIDs,
	RunE: func(cmd *cobra.Command, args []string) error {
		client, ok := cmd.Context().Value(keys.CLIENT_CONTEXT_KEY).(*client.Client)
		if !ok {
			return fmt.Errorf("invalid client in context")
		}

		jid := args[0]

		// Get the job type, so we can call the plugin command to override request details
		resp, err := client.Get(cmd.Context(), &daemon.GetReq{JID: jid})
		if err != nil {
			return err
		}
		jobType := resp.GetJob().GetType()

		if jobType != "process" {
			err = features.UnfreezeCmd.IfAvailable(
				func(name string, pluginCmd *cobra.Command) error {
					// Call the plugin command to override request details
					return pluginCmd.RunE(cmd, nil) // don't pass any args
				}, jobType,
			)
			if err != nil {
				return err
			}
		}

		// Since the request details have been modified by the plugin command, we need to fetch it
		req, ok := cmd.Context().Value(keys.UNFREEZE_REQ_CONTEXT_KEY).(*daemon.DumpReq)
		if !ok {
			return fmt.Errorf("invalid unfreeze request in context")
		}

		if req.Details == nil {
			req.Details = &daemon.Details{}
		}
		req.Details.JID = proto.String(jid)

		ctx := context.WithValue(cmd.Context(), keys.UNFREEZE_REQ_CONTEXT_KEY, req)
		cmd.SetContext(ctx)

		return nil
	},
}

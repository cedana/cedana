package cmd

import (
	"context"
	"fmt"
	"strconv"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/internal/features"
	"github.com/cedana/cedana/pkg/config"
	"github.com/cedana/cedana/pkg/flags"
	"github.com/cedana/cedana/pkg/keys"
	"github.com/spf13/cobra"
)

func init() {
	manageCmd.AddCommand(processManageCmd)

	// Add common flags
	manageCmd.PersistentFlags().StringP(flags.JidFlag.Full, flags.JidFlag.Short, "", "job id")
	manageCmd.PersistentFlags().
		BoolP(flags.GpuEnabledFlag.Full, flags.GpuEnabledFlag.Short, false, "enable GPU support")

	///////////////////////////////////////////
	// Add modifications from supported plugins
	///////////////////////////////////////////

	features.ManageCmd.IfAvailable(
		func(name string, pluginCmd *cobra.Command) error {
			manageCmd.AddCommand(pluginCmd)
			return nil
		},
	)
}

// Parent manage command
var manageCmd = &cobra.Command{
	Use:   "manage",
	Short: "Manage an existing process/container (create a job)",
	Args:  cobra.ArbitraryArgs,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		jid, _ := cmd.Flags().GetString(flags.JidFlag.Full)
		gpuEnabled, _ := cmd.Flags().GetBool(flags.GpuEnabledFlag.Full)

		// Create half-baked request
		req := &daemon.RunReq{
			JID:        jid,
			GPUEnabled: gpuEnabled,
		}

		ctx := context.WithValue(cmd.Context(), keys.RUN_REQ_CONTEXT_KEY, req)
		cmd.SetContext(ctx)

		return nil
	},

	//******************************************************************************************
	// Let subcommands (incl. from plugins) add details to the request, in the `RunE` hook.
	// Finally, we send the request to the server in the PersistentPostRun hook.
	// The server will make sure to handle it appropriately using any required plugins.
	//******************************************************************************************

	PersistentPostRunE: func(cmd *cobra.Command, args []string) (err error) {
		useVSOCK := config.Global.UseVSOCK
		var client *Client

		if useVSOCK {
			client, err = NewVSOCKClient(config.Global.ContextID, config.Global.Port)
		} else {
			client, err = NewClient(config.Global.Host, config.Global.Port)
		}
		if err != nil {
			return fmt.Errorf("Error creating client: %v", err)
		}
		defer client.Close()

		// Assuming request is now ready to be sent to the server
		req, ok := cmd.Context().Value(keys.RUN_REQ_CONTEXT_KEY).(*daemon.RunReq)
		if !ok {
			return fmt.Errorf("invalid request in context")
		}

		resp, err := client.Manage(cmd.Context(), req)
		if err != nil {
			return err
		}

		if config.Global.Profiling.Enabled && resp.Profiling != nil {
			printProfilingData(resp.Profiling)
		}

		fmt.Printf("Managing %s PID %d\n", req.Type, resp.PID)

		return nil
	},
}

////////////////////
/// Subcommands  ///
////////////////////

var processManageCmd = &cobra.Command{
	Use:   "process <path> [args...]",
	Short: "Managed existing process (job)",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		req, ok := cmd.Context().Value(keys.RUN_REQ_CONTEXT_KEY).(*daemon.RunReq)
		if !ok {
			return fmt.Errorf("invalid request in context")
		}

		pid, err := strconv.ParseUint(args[0], 10, 32)
		if err != nil {
			return fmt.Errorf("invalid pid: %v", err)
		}

		req.Type = "process"
		req.Details = &daemon.Details{
			Process: &daemon.Process{
				PID: uint32(pid),
			},
		}

		ctx := context.WithValue(cmd.Context(), keys.RUN_REQ_CONTEXT_KEY, req)
		cmd.SetContext(ctx)

		return nil
	},
}

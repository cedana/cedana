package cmd

import (
	"context"
	"fmt"
	"os"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/internal/config"
	"github.com/cedana/cedana/pkg/flags"
	"github.com/cedana/cedana/pkg/keys"
	"github.com/cedana/cedana/pkg/plugins"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/spf13/cobra"
)

// Pluggable features
const featureRunCmd plugins.Feature[*cobra.Command] = "RunCmd"

func init() {
	runCmd.AddCommand(processRunCmd)

	// Add common flags
	runCmd.PersistentFlags().StringP(flags.JidFlag.Full, flags.JidFlag.Short, "", "job id")
	runCmd.PersistentFlags().
		BoolP(flags.GpuEnabledFlag.Full, flags.GpuEnabledFlag.Short, false, "enable GPU support")
	runCmd.PersistentFlags().
		BoolP(flags.AttachFlag.Full, flags.AttachFlag.Short, false, "attach stdin/out/err")
	runCmd.PersistentFlags().
		StringP(flags.LogFlag.Full, flags.LogFlag.Short, "", "log path to forward stdout/err")
	runCmd.MarkFlagsMutuallyExclusive(
		flags.AttachFlag.Full,
		flags.LogFlag.Full,
	) // only one of these can be set

	// Add aliases
	rootCmd.AddCommand(utils.AliasOf(processRunCmd, "exec"))

	///////////////////////////////////////////
	// Add modifications from supported plugins
	///////////////////////////////////////////

	featureRunCmd.IfAvailable(
		func(name string, pluginCmd *cobra.Command) error {
			runCmd.AddCommand(pluginCmd)
			return nil
		},
	)
}

// Parent run command
var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run a managed process/container (create a job)",
	Args:  cobra.ArbitraryArgs,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		jid, _ := cmd.Flags().GetString(flags.JidFlag.Full)
		gpuEnabled, _ := cmd.Flags().GetBool(flags.GpuEnabledFlag.Full)
		log, _ := cmd.Flags().GetString(flags.LogFlag.Full)
		attach, _ := cmd.Flags().GetBool(flags.AttachFlag.Full)

		// Create half-baked request
		req := &daemon.RunReq{
			JID:        jid,
			Log:        log,
			GPUEnabled: gpuEnabled,
			Attachable: attach,
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
		useVSOCK, _ := cmd.Flags().GetBool(flags.UseVSOCKFlag.Full)
		var client *Client

		if useVSOCK {
			client, err = NewVSOCKClient(config.Get(config.VSOCK_CONTEXT_ID), config.Get(config.PORT))
		} else {
			client, err = NewClient(config.Get(config.HOST), config.Get(config.PORT))
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

		resp, err := client.Run(cmd.Context(), req)
		if err != nil {
			return err
		}

		if req.Attachable {
			return client.Attach(cmd.Context(), &daemon.AttachReq{PID: resp.PID})
		}

		fmt.Printf("Running managed PID %d\n", resp.PID)

		return nil
	},
}

////////////////////
/// Subcommands  ///
////////////////////

var processRunCmd = &cobra.Command{
	Use:   "process <path> [args...]",
	Short: "Run a managed process (job)",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		req, ok := cmd.Context().Value(keys.RUN_REQ_CONTEXT_KEY).(*daemon.RunReq)
		if !ok {
			return fmt.Errorf("invalid request in context")
		}

		path := args[0]
		args = args[1:]
		env := os.Environ()
		wd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("Error getting working directory: %v", err)
		}
		groups, err := os.Getgroups()
		if err != nil {
			return fmt.Errorf("Error getting groups: %v", err)
		}

		req.Type = "process"
		req.Details = &daemon.Details{
			ProcessRun: &daemon.RunDetails{
				Path:       path,
				Args:       args,
				Env:        env,
				WorkingDir: wd,
				UID:        int32(os.Getuid()),
				GID:        int32(os.Getgid()),
				Groups:     utils.Int32Slice(groups),
			},
		}

		ctx := context.WithValue(cmd.Context(), keys.RUN_REQ_CONTEXT_KEY, req)
		cmd.SetContext(ctx)

		return nil
	},
}

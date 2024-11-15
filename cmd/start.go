package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/cedana/cedana/internal/config"
	"github.com/cedana/cedana/internal/plugins"
	"github.com/cedana/cedana/pkg/api/daemon"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/spf13/cobra"
)

func init() {
	startCmd.AddCommand(processStartCmd)

	// Add common flags
	startCmd.PersistentFlags().StringP(types.JidFlag.Full, types.JidFlag.Short, "", "job id")
	startCmd.PersistentFlags().
		BoolP(types.GpuEnabledFlag.Full, types.GpuEnabledFlag.Short, false, "enable GPU support")
	startCmd.PersistentFlags().
		BoolP(types.AttachFlag.Full, types.AttachFlag.Short, false, "attach stdin/out/err")
	startCmd.PersistentFlags().
		StringP(types.LogFlag.Full, types.LogFlag.Short, "", "log path to forward stdout/err")
	startCmd.MarkFlagsMutuallyExclusive(
		types.AttachFlag.Full,
		types.LogFlag.Full,
	) // only one of these can be set

	// Sync flags with aliases
	execCmd.Flags().AddFlagSet(startCmd.PersistentFlags())

	///////////////////////////////////////////
	// Add modifications from supported plugins
	///////////////////////////////////////////

	plugins.IfFeatureAvailable(
		plugins.FEATURE_START_CMD,
		func(name string, pluginCmd **cobra.Command) error {
			startCmd.AddCommand(*pluginCmd)
			return nil
		},
	)
}

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start a managed process/container (create a job)",
	Args:  cobra.ArbitraryArgs,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		jid, _ := cmd.Flags().GetString(types.JidFlag.Full)
		gpuEnabled, _ := cmd.Flags().GetBool(types.GpuEnabledFlag.Full)
		log, _ := cmd.Flags().GetString(types.LogFlag.Full)
		attach, _ := cmd.Flags().GetBool(types.AttachFlag.Full)

		// Create half-baked request
		req := &daemon.StartReq{
			JID:        jid,
			Log:        log,
			GPUEnabled: gpuEnabled,
			Attach:     attach,
		}

		ctx := context.WithValue(cmd.Context(), types.START_REQ_CONTEXT_KEY, req)
		cmd.SetContext(ctx)

		return nil
	},

	//******************************************************************************************
	// Let subcommands (incl. from plugins) add details to the request, in the `RunE` hook.
	// Finally, we send the request to the server in the PersistentPostRun hook.
	// The server will make sure to handle it appropriately using any required plugins.
	//******************************************************************************************

	PersistentPostRunE: func(cmd *cobra.Command, args []string) error {
		client, err := NewClient(config.Get(config.HOST), config.Get(config.PORT))
		if err != nil {
			return fmt.Errorf("Error creating client: %v", err)
		}
		defer client.Close()

		// Assuming request is now ready to be sent to the server
		req := utils.GetContextValSafe(
			cmd.Context(),
			types.START_REQ_CONTEXT_KEY,
			&daemon.StartReq{},
		)

		resp, err := client.Start(cmd.Context(), req)
		if err != nil {
			return err
		}

		if req.Attach {
			return client.Attach(cmd.Context(), &daemon.AttachReq{PID: resp.PID})
		}

		fmt.Printf(resp.Message)
		fmt.Printf("Started managing job %s, PID %d\n", resp.JID, resp.PID)

		return nil
	},
}

////////////////////
/// Subcommands  ///
////////////////////

var processStartCmd = &cobra.Command{
	Use:   "process <path> [args...]",
	Short: "Start a managed process (job)",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		req := utils.GetContextValSafe(
			cmd.Context(),
			types.START_REQ_CONTEXT_KEY,
			&daemon.StartReq{},
		)

		path := args[0]
		args = args[1:]
		env := os.Environ()
		wd, _ := os.Getwd()

		req.Type = "process"
		req.Details = &daemon.Details{
			ProcessStart: &daemon.ProcessStartDetails{
				Path:       path,
				Args:       args,
				Env:        env,
				WorkingDir: wd,
			},
		}

		ctx := context.WithValue(cmd.Context(), types.START_REQ_CONTEXT_KEY, req)
		cmd.SetContext(ctx)

		return nil
	},
}

////////////////////
///// Aliases //////
////////////////////

var execCmd = &cobra.Command{
	Use:   utils.AliasCommandUse(processStartCmd, "exec"),
	Short: processStartCmd.Short,
	Long:  processStartCmd.Long,
	Args:  processStartCmd.Args,
	RunE:  utils.AliasCommandRunE(processStartCmd),
}

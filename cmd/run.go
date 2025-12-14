package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/internal/cedana"
	"github.com/cedana/cedana/pkg/client"
	"github.com/cedana/cedana/pkg/config"
	"github.com/cedana/cedana/pkg/features"
	"github.com/cedana/cedana/pkg/flags"
	"github.com/cedana/cedana/pkg/keys"
	"github.com/cedana/cedana/pkg/profiling"
	"github.com/cedana/cedana/pkg/style"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/spf13/cobra"
)

func init() {
	runCmd.AddCommand(processRunCmd)

	// Add common flags
	runCmd.PersistentFlags().
		StringP(flags.PidFileFlag.Full, flags.PidFileFlag.Short, "", "file to write PID to")
	runCmd.PersistentFlags().BoolP(flags.NoServerFlag.Full, flags.NoServerFlag.Short, false, "run without server")
	runCmd.PersistentFlags().StringP(flags.JidFlag.Full, flags.JidFlag.Short, "", "job id")
	runCmd.PersistentFlags().
		BoolP(flags.GpuEnabledFlag.Full, flags.GpuEnabledFlag.Short, false, "enable GPU support")
	runCmd.PersistentFlags().
		BoolP(flags.GpuTracingFlag.Full, flags.GpuTracingFlag.Short, false, "enable GPU tracing")
	runCmd.PersistentFlags().
		StringP(flags.GpuIdFlag.Full, flags.GpuIdFlag.Short, "", "specify existing GPU controller ID to attach (internal use only)")
	runCmd.PersistentFlags().
		BoolP(flags.AttachFlag.Full, flags.AttachFlag.Short, false, "attach stdin/out/err")
	runCmd.PersistentFlags().
		BoolP(flags.AttachableFlag.Full, flags.AttachableFlag.Short, false, "make it attachable, but don't attach")
	runCmd.PersistentFlags().
		StringP(flags.OutFlag.Full, flags.OutFlag.Short, "", "file to forward stdout/err")
	runCmd.MarkFlagsMutuallyExclusive(
		flags.AttachFlag.Full,
		flags.OutFlag.Full,
	) // only one of these can be set

	processRunCmd.PersistentFlags().
		BoolP(flags.AsRootFlag.Full, flags.AsRootFlag.Short, false, "run as root")

	// Add aliases
	rootCmd.AddCommand(utils.AliasOf(processRunCmd, "exec"))

	///////////////////////////////////////////
	// Add subcommands from supported plugins
	///////////////////////////////////////////

	features.RunCmd.IfAvailable(
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
		gpuTracing, _ := cmd.Flags().GetBool(flags.GpuTracingFlag.Full)
		gpuID, _ := cmd.Flags().GetString(flags.GpuIdFlag.Full)
		out, _ := cmd.Flags().GetString(flags.OutFlag.Full)
		attach, _ := cmd.Flags().GetBool(flags.AttachFlag.Full)
		attachable, _ := cmd.Flags().GetBool(flags.AttachableFlag.Full)
		pidFile, _ := cmd.Flags().GetString(flags.PidFileFlag.Full)
		noServer, _ := cmd.Flags().GetBool(flags.NoServerFlag.Full)

		if noServer && (out != "" || attach || attachable) {
			fmt.Println(
				style.WarningColors.Sprintf(
					"When using `--%s`, flags `--%s`, `--%s`, and `--%s` are ignored as the standard output is copied to the caller.",
					flags.NoServerFlag.Full,
					flags.OutFlag.Full,
					flags.AttachFlag.Full,
					flags.AttachableFlag.Full,
				))
		}

		env := os.Environ()
		user, err := utils.GetCredentials()
		if err != nil {
			return fmt.Errorf("Error getting user credentials: %v", err)
		}

		// Create initial request
		req := &daemon.RunReq{
			JID:        jid,
			Log:        out,
			PidFile:    pidFile,
			GPUEnabled: gpuEnabled,
			GPUTracing: gpuTracing,
			GPUID:      gpuID,

			Attachable: attach || attachable,
			Action:     daemon.RunAction_START_NEW,
			Env:        env,
			UID:        user.Uid,
			GID:        user.Gid,
			Groups:     user.Groups,
			Details:    &daemon.Details{},
		}

		ctx := context.WithValue(cmd.Context(), keys.RUN_REQ_CONTEXT_KEY, req)
		cmd.SetContext(ctx)

		if noServer {
			return nil
		}

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
		ctx := cmd.Context()
		noServer, _ := cmd.Flags().GetBool(flags.NoServerFlag.Full)

		// Assuming request is now ready to be sent to the server
		req, ok := cmd.Context().Value(keys.RUN_REQ_CONTEXT_KEY).(*daemon.RunReq)
		if !ok {
			return fmt.Errorf("invalid request in context")
		}

		if noServer {
			cedana, err := cedana.New(ctx, "run")
			if err != nil {
				return fmt.Errorf("Error: failed to create cedana root: %v", err)
			}

			code, err := cedana.Run(req)
			if err != nil {
				cedana.Wait()
				return utils.GRPCErrorColored(err)
			}

			data := cedana.Finalize()
			if config.Global.Profiling.Enabled && data != nil {
				profiling.Print(data, features.Theme())
			}
			cedana.Wait()

			os.Exit(<-code)
		} else {
			client, ok := cmd.Context().Value(keys.CLIENT_CONTEXT_KEY).(*client.Client)
			if !ok {
				return fmt.Errorf("invalid client in context")
			}
			defer client.Close()

			// Assuming request is now ready to be sent to the server
			resp, data, err := client.Run(cmd.Context(), req)
			if err != nil {
				return err
			}

			if config.Global.Profiling.Enabled && data != nil {
				profiling.Print(data, features.Theme())
			}

			attach, _ := cmd.Flags().GetBool(flags.AttachFlag.Full)
			if attach {
				return client.Attach(cmd.Context(), &daemon.AttachReq{PID: resp.PID})
			}

			for _, message := range resp.GetMessages() {
				fmt.Println(message)
			}
		}

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

		asRoot, _ := cmd.Flags().GetBool(flags.AsRootFlag.Full)

		path := args[0]
		if fullPath, err := exec.LookPath(path); err == nil {
			path = fullPath
		} else {
			return err
		}

		args = args[1:]
		wd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("Error getting working directory: %v", err)
		}

		req.Type = "process"
		req.Details = &daemon.Details{
			Process: &daemon.Process{
				Path:       path,
				Args:       args,
				WorkingDir: wd,
			},
		}

		if asRoot {
			user := utils.GetRootCredentials()
			req.UID = user.Uid
			req.GID = user.Gid
			req.Groups = user.Groups
		}

		return nil
	},
}

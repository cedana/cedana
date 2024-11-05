package cmd

// By default, implements only the process restore subcommand.
// Other subcommands, for e.g. runc, are imported from installed plugins, as they could
// declare their own flags and subcommands. For runc, see plugins/runc/cmd/restore.go.

import (
	"context"
	"fmt"

	"github.com/cedana/cedana/internal/plugins"
	"github.com/cedana/cedana/pkg/api/daemon"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"google.golang.org/protobuf/proto"
)

func init() {
	restoreCmd.AddCommand(processRestoreCmd)
	restoreCmd.AddCommand(jobRestoreCmd)

	// Add common flags
	restoreCmd.PersistentFlags().StringP(types.PathFlag.Full, types.PathFlag.Short, "", "path of dump")
	restoreCmd.PersistentFlags().BoolP(types.StreamFlag.Full, types.StreamFlag.Short, false, "stream the dump using cedana-image-streamer")
	restoreCmd.PersistentFlags().BoolP(types.TcpEstablishedFlag.Full, types.TcpEstablishedFlag.Short, false, "restore tcp established connections")
	restoreCmd.PersistentFlags().BoolP(types.TcpCloseFlag.Full, types.TcpCloseFlag.Short, false, "allow listening TCP sockets to be exist on restore")
	restoreCmd.PersistentFlags().StringP(types.LogFlag.Full, types.LogFlag.Short, "", "log path to forward stdout/stderr")

	///////////////////////////////////////////
	// Add modifications from supported plugins
	///////////////////////////////////////////

	for name, p := range plugins.LoadedPlugins {
		defer plugins.RecoverFromPanic(name)
		if pluginCmdUntyped, err := p.Lookup(plugins.FEATURE_RESTORE_CMD); err == nil {
			// Add new subcommand from supported plugins
			pluginCmd, ok := pluginCmdUntyped.(**cobra.Command)
			if !ok {
				log.Debug().Str("plugin", name).Msgf("Provided %s is not a valid command", plugins.FEATURE_RESTORE_CMD)
				continue
			}
			restoreCmd.AddCommand(*pluginCmd)

			// Apply all the flags from the plugin command to job subcommand (as optional flags),
			// since the job subcommand can be used to restore any managed entity (even from plugins, like runc),
			// thus it could have specific CLI overrides from plugins.

			(*pluginCmd).Flags().VisitAll(func(f *pflag.Flag) {
				jobRestoreCmd.Flags().AddFlag(f)
				f.Usage = fmt.Sprintf("(%s) %s", name, f.Usage) // Add plugin name to usage
			})
		}
	}
}

var restoreCmd = &cobra.Command{
	Use:   "restore",
	Short: "Restore a container/process",
	Args:  cobra.ArbitraryArgs,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		path, _ := cmd.Flags().GetString(types.PathFlag.Full)
		stream, _ := cmd.Flags().GetBool(types.StreamFlag.Full)
		tcpEstablished, _ := cmd.Flags().GetBool(types.TcpEstablishedFlag.Full)
		tcpClose, _ := cmd.Flags().GetBool(types.TcpCloseFlag.Full)
		log, _ := cmd.Flags().GetString(types.LogFlag.Full)

		// Create half-baked request
		req := &daemon.RestoreReq{
			Path:   path,
			Stream: stream,
			Log:    log,
			Criu: &daemon.CriuOpts{
				TcpEstablished: tcpEstablished,
				TcpClose:       tcpClose,
			},
		}

		ctx := context.WithValue(cmd.Context(), types.RESTORE_REQ_CONTEXT_KEY, req)
		cmd.SetContext(ctx)

		return nil
	},

	//******************************************************************************************
	// Let subcommands (incl. from plugins) add details to the request, in the `RunE` hook.
	// Finally, we send the request to the server in the PersistentPostRun hook.
	// The server will make sure to handle it appropriately using any required plugins.
	//******************************************************************************************

	PersistentPostRunE: func(cmd *cobra.Command, args []string) error {
		port := viper.GetUint32("options.port")
		host := viper.GetString("options.host")

		client, err := NewClient(host, port)
		if err != nil {
			return fmt.Errorf("Error creating client: %v", err)
		}
		defer client.Close()

		// Assuming request is now ready to be sent to the server
		req := utils.GetContextValSafe(cmd.Context(), types.RESTORE_REQ_CONTEXT_KEY, &daemon.RestoreReq{})

		resp, err := client.Restore(cmd.Context(), req)
		if err != nil {
			return err
		}

		fmt.Printf(resp.Message)
		fmt.Printf("Restored successfully, PID: %d\n", resp.PID)

		return nil
	},
}

////////////////////
/// Subcommands  ///
////////////////////

var processRestoreCmd = &cobra.Command{
	Use:   "process",
	Short: "Restore a process",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		// All we need to do is modify the request type
		req := utils.GetContextValSafe(cmd.Context(), types.RESTORE_REQ_CONTEXT_KEY, &daemon.RestoreReq{})

		req.Type = "process"

		ctx := context.WithValue(cmd.Context(), types.RESTORE_REQ_CONTEXT_KEY, req)
		cmd.SetContext(ctx)

		return nil
	},
}

var jobRestoreCmd = &cobra.Command{
	Use:   "job <JID>",
	Short: "Restore a managed process/container (job)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// All we need to do is modify the request to include the job ID, and request type.
		req := utils.GetContextValSafe(cmd.Context(), types.RESTORE_REQ_CONTEXT_KEY, &daemon.RestoreReq{})

		if len(args) == 0 {
			return fmt.Errorf("Job ID is required")
		}
		jid := args[0]

		req.Type = "job"
		req.Details = &daemon.Details{JID: proto.String(jid)}

		ctx := context.WithValue(cmd.Context(), types.RESTORE_REQ_CONTEXT_KEY, req)
		cmd.SetContext(ctx)

		return nil
	},
}

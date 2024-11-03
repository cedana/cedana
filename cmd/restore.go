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
	"github.com/spf13/viper"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func init() {
	restoreCmd.AddCommand(processRestoreCmd)
	restoreCmd.AddCommand(jobRestoreCmd)

	// Add common flags
	restoreCmd.PersistentFlags().StringP(types.PathFlag.Full, types.PathFlag.Short, "", "path of dump")
	restoreCmd.PersistentFlags().BoolP(types.StreamFlag.Full, types.StreamFlag.Short, false, "stream the dump using cedana-image-streamer")
	restoreCmd.PersistentFlags().BoolP(types.TcpEstablishedFlag.Full, types.TcpEstablishedFlag.Short, false, "restore tcp established connections")
	restoreCmd.PersistentFlags().BoolP(types.TcpCloseFlag.Full, types.TcpCloseFlag.Short, false, "allow listening TCP sockets to be exist on restore")

	// Add commands from plugins
	for name, p := range plugins.LoadedPlugins {
		defer plugins.RecoverFromPanic(name)
		if pluginCmd, err := p.Lookup(plugins.FEATURE_RESTORE_CMD); err == nil {
			restoreCmd.AddCommand(*pluginCmd.(**cobra.Command))
		} else {
			log.Debug().Str("plugin", name).Err(err).Msg("Plugin does not provide a restore command")
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

		// Create half-baked request
		req := &daemon.RestoreReq{
			Path:   path,
			Stream: stream,
			Details: &daemon.RestoreDetails{
				Criu: &daemon.CriuOpts{
					TcpEstablished: tcpEstablished,
					TcpClose:       tcpClose,
				},
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
			st, ok := status.FromError(err)
			if ok {
				if st.Code() == codes.Unavailable {
					return fmt.Errorf("Daemon unavailable. Is it running?")
				} else {
					return fmt.Errorf("Failed: %v", st.Message())
				}
			}
			return fmt.Errorf("Unknown error: %v", err)
		}

		fmt.Printf(resp.Message)
		fmt.Printf("Restored successfully")

		return nil
	},
}

var processRestoreCmd = &cobra.Command{
	Use:   "process",
	Short: "Restore a process",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		// All we need to do is modify the request type
		req := utils.GetContextValSafe(cmd.Context(), types.RESTORE_REQ_CONTEXT_KEY, &daemon.RestoreReq{})

		req.Details = &daemon.RestoreDetails{
			Type: "process",
			Criu: req.GetDetails().GetCriu(),
		}

		ctx := context.WithValue(cmd.Context(), types.RESTORE_REQ_CONTEXT_KEY, req)
		cmd.SetContext(ctx)

		return nil
	},
}

var jobRestoreCmd = &cobra.Command{
	Use:   "job <JID>",
	Short: "Restore a managed process/container (job)",
	RunE: func(cmd *cobra.Command, args []string) error {
		return nil
	},
}

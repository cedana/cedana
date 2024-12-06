package cmd

// By default, implements only the process restore subcommand.
// Other subcommands, for e.g. runc, are imported from installed plugins, as they could
// declare their own flags and subcommands. For runc, see plugins/runc/cmd/restore.go.

import (
	"context"
	"fmt"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/internal/config"
	"github.com/cedana/cedana/pkg/flags"
	"github.com/cedana/cedana/pkg/keys"
	"github.com/cedana/cedana/pkg/plugins"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"google.golang.org/protobuf/proto"
)

// Pluggable features
const featureRestoreCmd plugins.Feature[*cobra.Command] = "RestoreCmd"

func init() {
	restoreCmd.AddCommand(processRestoreCmd)
	restoreCmd.AddCommand(jobRestoreCmd)

	// Add common flags
	restoreCmd.PersistentFlags().
		StringP(flags.PathFlag.Full, flags.PathFlag.Short, "", "path of dump")
	restoreCmd.PersistentFlags().
		BoolP(flags.StreamFlag.Full, flags.StreamFlag.Short, false, "stream the dump using cedana-image-streamer")
	restoreCmd.PersistentFlags().
		BoolP(flags.TcpEstablishedFlag.Full, flags.TcpEstablishedFlag.Short, false, "restore tcp established connections")
	restoreCmd.PersistentFlags().
		BoolP(flags.TcpCloseFlag.Full, flags.TcpCloseFlag.Short, false, "allow listening TCP sockets to be exist on restore")
	restoreCmd.PersistentFlags().
		BoolP(flags.LeaveStoppedFlag.Full, flags.LeaveStoppedFlag.Short, false, "leave the process stopped after restore")
	restoreCmd.PersistentFlags().
		BoolP(flags.FileLocksFlag.Full, flags.FileLocksFlag.Short, false, "restore file locks")
	restoreCmd.PersistentFlags().
		StringP(flags.LogFlag.Full, flags.LogFlag.Short, "", "log path to forward stdout/err")
	restoreCmd.PersistentFlags().
		BoolP(flags.AttachFlag.Full, flags.AttachFlag.Short, false, "attach stdin/out/err")
	restoreCmd.MarkFlagsMutuallyExclusive(
		flags.AttachFlag.Full,
		flags.LogFlag.Full,
	) // only one of these can be set

	///////////////////////////////////////////
	// Add modifications from supported plugins
	///////////////////////////////////////////

	featureRestoreCmd.IfAvailable(
		func(name string, pluginCmd *cobra.Command) error {
			restoreCmd.AddCommand(pluginCmd)

			// Apply all the flags from the plugin command to job subcommand (as optional flags),
			// since the job subcommand can be used to restore any managed entity (even from plugins, like runc),
			// thus it could have specific CLI overrides from plugins.

			(*pluginCmd).Flags().VisitAll(func(f *pflag.Flag) {
				jobRestoreCmd.Flags().AddFlag(f)
				f.Usage = fmt.Sprintf("(%s) %s", name, f.Usage) // Add plugin name to usage
			})
			return nil
		},
	)
}

// Parent restore command
var restoreCmd = &cobra.Command{
	Use:   "restore",
	Short: "Restore a container/process",
	Args:  cobra.ArbitraryArgs,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		path, _ := cmd.Flags().GetString(flags.PathFlag.Full)
		stream, _ := cmd.Flags().GetBool(flags.StreamFlag.Full)
		tcpEstablished, _ := cmd.Flags().GetBool(flags.TcpEstablishedFlag.Full)
		tcpClose, _ := cmd.Flags().GetBool(flags.TcpCloseFlag.Full)
		leaveStopped, _ := cmd.Flags().GetBool(flags.LeaveStoppedFlag.Full)
		fileLocks, _ := cmd.Flags().GetBool(flags.FileLocksFlag.Full)
		log, _ := cmd.Flags().GetString(flags.LogFlag.Full)
		attach, _ := cmd.Flags().GetBool(flags.AttachFlag.Full)

		// Create half-baked request
		req := &daemon.RestoreReq{
			Path:       path,
			Stream:     stream,
			Log:        log,
			Attachable: attach,
			Criu: &criu.CriuOpts{
				TcpEstablished: proto.Bool(tcpEstablished),
				TcpClose:       proto.Bool(tcpClose),
				LeaveStopped:   proto.Bool(leaveStopped),
				FileLocks:      proto.Bool(fileLocks),
			},
		}

		ctx := context.WithValue(cmd.Context(), keys.RESTORE_REQ_CONTEXT_KEY, req)
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
		req, ok := cmd.Context().Value(keys.RESTORE_REQ_CONTEXT_KEY).(*daemon.RestoreReq)
		if !ok {
			return fmt.Errorf("invalid restore request in context")
		}

		resp, err := client.Restore(cmd.Context(), req)
		if err != nil {
			return err
		}

		if req.Attachable {
			return client.Attach(cmd.Context(), &daemon.AttachReq{PID: resp.PID})
		}

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
		req, ok := cmd.Context().Value(keys.RESTORE_REQ_CONTEXT_KEY).(*daemon.RestoreReq)
		if !ok {
			return fmt.Errorf("invalid restore request in context")
		}

		req.Type = "process"

		ctx := context.WithValue(cmd.Context(), keys.RESTORE_REQ_CONTEXT_KEY, req)
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
		req, ok := cmd.Context().Value(keys.RESTORE_REQ_CONTEXT_KEY).(*daemon.RestoreReq)
		if !ok {
			return fmt.Errorf("invalid restore request in context")
		}

		if len(args) == 0 {
			return fmt.Errorf("Job ID is required")
		}
		jid := args[0]

		req.Details = &daemon.Details{JID: proto.String(jid)}

		ctx := context.WithValue(cmd.Context(), keys.RESTORE_REQ_CONTEXT_KEY, req)
		cmd.SetContext(ctx)

		return nil
	},
}

package cmd

// By default, implements only the process restore subcommand.
// Other subcommands, for e.g. runc, are imported from installed plugins, as they could
// declare their own flags and subcommands. For runc, see plugins/runc/cmd/restore.go.

import (
	"context"
	"fmt"
	"os"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/pkg/client"
	"github.com/cedana/cedana/pkg/config"
	"github.com/cedana/cedana/pkg/features"
	"github.com/cedana/cedana/pkg/flags"
	"github.com/cedana/cedana/pkg/keys"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"google.golang.org/protobuf/proto"
)

func init() {
	restoreCmd.AddCommand(processRestoreCmd)
	restoreCmd.AddCommand(jobRestoreCmd)

	// Add common flags
	restoreCmd.PersistentFlags().
		StringP(flags.PathFlag.Full, flags.PathFlag.Short, "", "path of dump")
	restoreCmd.PersistentFlags().
		Int32P(flags.StreamFlag.Full, flags.StreamFlag.Short, 0, "stream the restore (using <n> parallel streams)")
	restoreCmd.PersistentFlags().
		BoolP(flags.TcpEstablishedFlag.Full, flags.TcpEstablishedFlag.Short, false, "restore tcp established connections")
	restoreCmd.PersistentFlags().
		BoolP(flags.TcpCloseFlag.Full, flags.TcpCloseFlag.Short, false, "allow listening TCP sockets to exist on restore")
	restoreCmd.PersistentFlags().
		BoolP(flags.LeaveStoppedFlag.Full, flags.LeaveStoppedFlag.Short, false, "leave the process stopped after restore")
	restoreCmd.PersistentFlags().
		BoolP(flags.FileLocksFlag.Full, flags.FileLocksFlag.Short, false, "restore file locks")
	restoreCmd.PersistentFlags().
		StringSliceP(flags.ExternalFlag.Full, flags.ExternalFlag.Short, nil, "resources from external namespaces (can be multiple)")
	restoreCmd.PersistentFlags().
		StringP(flags.LogFlag.Full, flags.LogFlag.Short, "", "log path to forward stdout/err")
	restoreCmd.PersistentFlags().
		BoolP(flags.AttachFlag.Full, flags.AttachFlag.Short, false, "attach stdin/out/err")
	restoreCmd.PersistentFlags().
		BoolP(flags.ShellJobFlag.Full, flags.ShellJobFlag.Short, false, "process is not session leader (shell job)")
	restoreCmd.PersistentFlags().
		BoolP(flags.LinkRemapFlag.Full, flags.LinkRemapFlag.Short, false, "remap links to invisible files during restore")
	restoreCmd.MarkFlagsMutuallyExclusive(
		flags.AttachFlag.Full,
		flags.LogFlag.Full,
	) // only one of these can be set

	// Bind to config
	viper.BindPFlag("checkpoint.stream", restoreCmd.PersistentFlags().Lookup(flags.StreamFlag.Full))

	///////////////////////////////////////////
	// Add subcommands from supported plugins
	///////////////////////////////////////////

	features.RestoreCmd.IfAvailable(
		func(name string, pluginCmd *cobra.Command) error {
			restoreCmd.AddCommand(pluginCmd)

			// Apply all the flags from the plugin command to job subcommand (as optional flags),
			// since the job subcommand can be used to restore any managed entity (even from plugins, like runc),
			// thus it could have specific CLI overrides from plugins.

			(*pluginCmd).Flags().VisitAll(func(f *pflag.Flag) {
				newFlag := *f
				jobRestoreCmd.Flags().AddFlag(&newFlag)
				newFlag.Usage = fmt.Sprintf("(%s) %s", name, f.Usage) // Add plugin name to usage
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
		stream, _ := cmd.Flags().GetInt32(flags.StreamFlag.Full)
		tcpEstablished, _ := cmd.Flags().GetBool(flags.TcpEstablishedFlag.Full)
		tcpClose, _ := cmd.Flags().GetBool(flags.TcpCloseFlag.Full)
		leaveStopped, _ := cmd.Flags().GetBool(flags.LeaveStoppedFlag.Full)
		fileLocks, _ := cmd.Flags().GetBool(flags.FileLocksFlag.Full)
		external, _ := cmd.Flags().GetStringSlice(flags.ExternalFlag.Full)
		shellJob, _ := cmd.Flags().GetBool(flags.ShellJobFlag.Full)
		log, _ := cmd.Flags().GetString(flags.LogFlag.Full)
		attach, _ := cmd.Flags().GetBool(flags.AttachFlag.Full)
		linkRemap, _ := cmd.Flags().GetBool(flags.LinkRemapFlag.Full)

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
				External:       external,
				ShellJob:       proto.Bool(shellJob),
				LinkRemap:      proto.Bool(linkRemap),
			},
		}

		if req.Env == nil {
			req.Env = os.Environ()
		}

		ctx := context.WithValue(cmd.Context(), keys.RESTORE_REQ_CONTEXT_KEY, req)
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
		req, ok := cmd.Context().Value(keys.RESTORE_REQ_CONTEXT_KEY).(*daemon.RestoreReq)
		if !ok {
			return fmt.Errorf("invalid restore request in context")
		}

		resp, profiling, err := client.Restore(cmd.Context(), req)
		if err != nil {
			return err
		}

		if config.Global.Profiling.Enabled && profiling != nil {
			printProfilingData(profiling)
		}

		if req.Attachable {
			return client.Attach(cmd.Context(), &daemon.AttachReq{PID: resp.PID})
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
	Use:               "job <JID>",
	Short:             "Restore a managed process/container (job)",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: ValidJIDs,
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
			err = features.RestoreCmd.IfAvailable(
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
		req, ok := cmd.Context().Value(keys.RESTORE_REQ_CONTEXT_KEY).(*daemon.RestoreReq)
		if !ok {
			return fmt.Errorf("invalid restore request in context")
		}

		if req.Details == nil {
			req.Details = &daemon.Details{}
		}
		req.Details.JID = proto.String(jid)

		ctx := context.WithValue(cmd.Context(), keys.RESTORE_REQ_CONTEXT_KEY, req)
		cmd.SetContext(ctx)

		return nil
	},
}

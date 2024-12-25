package cmd

// By default, implements only the process dump subcommand.
// Other subcommands, for e.g. runc, are imported from installed plugins, as they could
// declare their own flags and subcommands. For runc, see plugins/runc/cmd/dump.go.

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/internal/features"
	"github.com/cedana/cedana/pkg/config"
	"github.com/cedana/cedana/pkg/flags"
	"github.com/cedana/cedana/pkg/keys"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"google.golang.org/protobuf/proto"
)

func init() {
	dumpCmd.AddCommand(processDumpCmd)
	dumpCmd.AddCommand(jobDumpCmd)

	// Add common flags
	dumpCmd.PersistentFlags().
		StringP(flags.DirFlag.Full, flags.DirFlag.Short, "", "directory to dump to")
	dumpCmd.MarkPersistentFlagDirname(flags.DirFlag.Full)
	dumpCmd.PersistentFlags().
		BoolP(flags.StreamFlag.Full, flags.StreamFlag.Short, false, "stream the dump using cedana-image-streamer")
	dumpCmd.PersistentFlags().
		BoolP(flags.LeaveRunningFlag.Full, flags.LeaveRunningFlag.Short, false, "leave the process running after dump")
	dumpCmd.PersistentFlags().
		BoolP(flags.TcpEstablishedFlag.Full, flags.TcpEstablishedFlag.Short, false, "dump tcp established connections")
	dumpCmd.PersistentFlags().
		BoolP(flags.TcpCloseFlag.Full, flags.TcpCloseFlag.Short, false, "close tcp connections")
	dumpCmd.PersistentFlags().
		BoolP(flags.TcpSkipInFlightFlag.Full, flags.TcpSkipInFlightFlag.Short, false, "skip in-flight tcp connections")
	dumpCmd.PersistentFlags().
		BoolP(flags.FileLocksFlag.Full, flags.FileLocksFlag.Short, false, "dump file locks")
	dumpCmd.PersistentFlags().
		StringP(flags.ExternalFlag.Full, flags.ExternalFlag.Short, "", "resources from external namespaces (comma-separated)")
	dumpCmd.PersistentFlags().
		BoolP(flags.ShellJobFlag.Full, flags.ShellJobFlag.Short, false, "process is not session leader (shell job)")

	// Bind to config
	viper.BindPFlag("checkpoints.dir", dumpCmd.PersistentFlags().Lookup(flags.DirFlag.Full))
	viper.BindPFlag("criu.leave_running", dumpCmd.PersistentFlags().Lookup(flags.LeaveRunningFlag.Full))

	///////////////////////////////////////////
	// Add modifications from supported plugins
	///////////////////////////////////////////

	features.DumpCmd.IfAvailable(
		func(name string, pluginCmd *cobra.Command) error {
			dumpCmd.AddCommand(pluginCmd)

			// Apply all the flags from the plugin command to job subcommand (as optional flags),
			// since the job subcommand can be used to dump any managed entity (even from plugins, like runc),
			// thus it could have specific CLI overrides from plugins.

			(*pluginCmd).Flags().VisitAll(func(f *pflag.Flag) {
				newFlag := *f
				jobDumpCmd.Flags().AddFlag(&newFlag)
				newFlag.Usage = fmt.Sprintf("(%s) %s", name, f.Usage) // Add plugin name to usage
			})
			return nil
		},
	)
}

// Parent dump command
var dumpCmd = &cobra.Command{
	Use:   "dump",
	Short: "Dump a container/process",
	Args:  cobra.ArbitraryArgs,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		dir := config.Global.Checkpoints.Dir
		leaveRunning := config.Global.CRIU.LeaveRunning
		stream, _ := cmd.Flags().GetBool(flags.StreamFlag.Full)
		tcpEstablished, _ := cmd.Flags().GetBool(flags.TcpEstablishedFlag.Full)
		tcpClose, _ := cmd.Flags().GetBool(flags.TcpCloseFlag.Full)
		tcpSkipInFlight, _ := cmd.Flags().GetBool(flags.TcpSkipInFlightFlag.Full)
		fileLocks, _ := cmd.Flags().GetBool(flags.FileLocksFlag.Full)
		external, _ := cmd.Flags().GetString(flags.ExternalFlag.Full)
		shellJob, _ := cmd.Flags().GetBool(flags.ShellJobFlag.Full)

		// Create half-baked request
		req := &daemon.DumpReq{
			Dir:    dir,
			Stream: stream,
			Criu: &criu.CriuOpts{
				LeaveRunning:    proto.Bool(leaveRunning),
				TcpEstablished:  proto.Bool(tcpEstablished),
				TcpClose:        proto.Bool(tcpClose),
				TcpSkipInFlight: proto.Bool(tcpSkipInFlight),
				FileLocks:       proto.Bool(fileLocks),
				External:        strings.Split(external, ","),
				ShellJob:        proto.Bool(shellJob),
			},
		}

		ctx := context.WithValue(cmd.Context(), keys.DUMP_REQ_CONTEXT_KEY, req)
		cmd.SetContext(ctx)

		useVSOCK := config.Global.UseVSOCK
		var client *Client
		var err error

		if useVSOCK {
			client, err = NewVSOCKClient(config.Global.ContextID, config.Global.Port)
		} else {
			client, err = NewClient(config.Global.Host, config.Global.Port)
		}

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
		client, ok := cmd.Context().Value(keys.CLIENT_CONTEXT_KEY).(*Client)
		if !ok {
			return fmt.Errorf("invalid client in context")
		}
		defer client.Close()

		// Assuming request is now ready to be sent to the server
		req, ok := cmd.Context().Value(keys.DUMP_REQ_CONTEXT_KEY).(*daemon.DumpReq)
		if !ok {
			return fmt.Errorf("invalid request in context")
		}

		resp, profiling, err := client.Dump(cmd.Context(), req)
		if err != nil {
			return err
		}

		if config.Global.Profiling.Enabled && profiling != nil {
			printProfilingData(profiling)
		}

		fmt.Printf("Dumped to %s\n", resp.Path)

		return nil
	},
}

////////////////////
/// Subcommands  ///
////////////////////

var processDumpCmd = &cobra.Command{
	Use:   "process <PID>",
	Short: "Dump a process",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// All we need to do is modify the request to include the PID of the process to dump.
		// And modify the request type.
		req, ok := cmd.Context().Value(keys.DUMP_REQ_CONTEXT_KEY).(*daemon.DumpReq)
		if !ok {
			return fmt.Errorf("invalid request in context")
		}

		pid, err := strconv.ParseUint(args[0], 10, 32)
		if err != nil {
			return fmt.Errorf("invalid pid: %v", err)
		}

		req.Type = "process"
		req.Details = &daemon.Details{Process: &daemon.Process{PID: uint32(pid)}}

		ctx := context.WithValue(cmd.Context(), keys.DUMP_REQ_CONTEXT_KEY, req)
		cmd.SetContext(ctx)

		return nil
	},
}

var jobDumpCmd = &cobra.Command{
	Use:               "job <JID>",
	Short:             "Dump a managed process/container (job)",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: RunningJIDs,
	RunE: func(cmd *cobra.Command, args []string) error {
		client, ok := cmd.Context().Value(keys.CLIENT_CONTEXT_KEY).(*Client)
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
			err = features.DumpCmd.IfAvailable(
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
		req, ok := cmd.Context().Value(keys.DUMP_REQ_CONTEXT_KEY).(*daemon.DumpReq)
		if !ok {
			return fmt.Errorf("invalid dump request in context")
		}

		if req.Details == nil {
			req.Details = &daemon.Details{}
		}
		req.Details.JID = proto.String(jid)

		ctx := context.WithValue(cmd.Context(), keys.DUMP_REQ_CONTEXT_KEY, req)
		cmd.SetContext(ctx)

		return nil
	},
}

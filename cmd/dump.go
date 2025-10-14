package cmd

// By default, implements only the process dump subcommand.
// Other subcommands, for e.g. runc, are imported from installed plugins, as they could
// declare their own flags and subcommands. For runc, see plugins/runc/cmd/dump.go.

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/pkg/client"
	"github.com/cedana/cedana/pkg/config"
	"github.com/cedana/cedana/pkg/features"
	"github.com/cedana/cedana/pkg/flags"
	"github.com/cedana/cedana/pkg/keys"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"google.golang.org/protobuf/proto"
)

func init() {
	dumpCmd.AddCommand(processDumpCmd)
	dumpCmd.AddCommand(jobDumpCmd)

	// Add common flags
	dumpCmd.PersistentFlags().
		StringP(flags.DirFlag.Full, flags.DirFlag.Short, "", "directory to dump into")
	dumpCmd.PersistentFlags().
		StringP(flags.NameFlag.Full, "", "", "name of the dump")
	dumpCmd.MarkPersistentFlagDirname(flags.DirFlag.Full)
	dumpCmd.PersistentFlags().
		StringP(flags.CompressionFlag.Full, flags.CompressionFlag.Short, "", "compression algorithm (none, tar, gzip, lz4, zlib)")
	dumpCmd.PersistentFlags().
		Int32P(flags.StreamsFlag.Full, flags.StreamsFlag.Short, 0, "number of streams to use for dump (0 for no streaming)")
	dumpCmd.PersistentFlags().
		StringP(flags.CriuOptsFlag.Full, flags.CriuOptsFlag.Short, "", "criu options JSON (overriddes individual CRIU flags)")
	dumpCmd.PersistentFlags().
		BoolP(flags.LeaveRunningFlag.Full, flags.LeaveRunningFlag.Short, false, "leave the process running after dump")
	dumpCmd.PersistentFlags().
		BoolP(flags.TcpEstablishedFlag.Full, flags.TcpEstablishedFlag.Short, false, "dump tcp established connections")
	dumpCmd.PersistentFlags().
		BoolP(flags.TcpSkipInFlightFlag.Full, flags.TcpSkipInFlightFlag.Short, false, "skip in-flight tcp connections")
	dumpCmd.PersistentFlags().
		BoolP(flags.FileLocksFlag.Full, flags.FileLocksFlag.Short, false, "dump file locks")
	dumpCmd.PersistentFlags().
		StringSliceP(flags.ExternalFlag.Full, flags.ExternalFlag.Short, nil, "resources from external namespaces (can be multiple)")
	dumpCmd.PersistentFlags().
		BoolP(flags.ShellJobFlag.Full, flags.ShellJobFlag.Short, false, "process is not session leader (shell job)")
	dumpCmd.PersistentFlags().
		BoolP(flags.LinkRemapFlag.Full, flags.LinkRemapFlag.Short, false, "remap links to files in the dump")
	dumpCmd.PersistentFlags().
		StringP(flags.GpuFreezeTypeFlag.Full, flags.GpuFreezeTypeFlag.Short, "", "GPU freeze type (IPC, NCCL)")

	///////////////////////////////////////////
	// Add subcommands from supported plugins
	///////////////////////////////////////////

	features.DumpCmd.IfAvailable(
		func(name string, pluginCmd *cobra.Command) error {
			dumpCmd.AddCommand(pluginCmd)

			// Apply all the flags from the plugin command to job subcommand (as optional flags),
			// since the job subcommand can be used to dump any managed entity (even from plugins, like runc),
			// thus it could have specific CLI overrides from plugins.

			(*pluginCmd).Flags().VisitAll(func(f *pflag.Flag) {
				newFlag := *f
				if jobDumpCmd.Flags().Lookup(newFlag.Name) == nil {
					jobDumpCmd.Flags().AddFlag(&newFlag)
				}
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
		dir, _ := cmd.Flags().GetString(flags.DirFlag.Full)
		name, _ := cmd.Flags().GetString(flags.NameFlag.Full)
		compression, _ := cmd.Flags().GetString(flags.CompressionFlag.Full)
		streams, _ := cmd.Flags().GetInt32(flags.StreamsFlag.Full)
		gpuFreezeType, _ := cmd.Flags().GetString(flags.GpuFreezeTypeFlag.Full)

		external, _ := cmd.Flags().GetStringSlice(flags.ExternalFlag.Full)
		shellJob, _ := cmd.Flags().GetBool(flags.ShellJobFlag.Full)
		linkRemap, _ := cmd.Flags().GetBool(flags.LinkRemapFlag.Full)
		leaveRunning, _ := cmd.Flags().GetBool(flags.LeaveRunningFlag.Full)
		tcpEstablished, _ := cmd.Flags().GetBool(flags.TcpEstablishedFlag.Full)
		tcpSkipInFlight, _ := cmd.Flags().GetBool(flags.TcpSkipInFlightFlag.Full)
		fileLocks, _ := cmd.Flags().GetBool(flags.FileLocksFlag.Full)
		criuOptsJSON, _ := cmd.Flags().GetString(flags.CriuOptsFlag.Full)

		criuOpts := &criu.CriuOpts{
			TcpEstablished:  proto.Bool(tcpEstablished),
			TcpSkipInFlight: proto.Bool(tcpSkipInFlight),
			LeaveRunning:    proto.Bool(leaveRunning),
			FileLocks:       proto.Bool(fileLocks),
			ShellJob:        proto.Bool(shellJob),
			LinkRemap:       proto.Bool(linkRemap),
			External:        external,
		}
		if criuOptsJSON != "" {
			err := json.Unmarshal([]byte(criuOptsJSON), criuOpts)
			if err != nil {
				return fmt.Errorf("Error parsing CRIU options JSON: %v", err)
			}
		}

		// Create half-baked request
		req := &daemon.DumpReq{
			Dir:           dir,
			Name:          name,
			Compression:   compression,
			Streams:       int32(streams),
			Criu:          criuOpts,
			Action:        daemon.DumpAction_DUMP,
			GPUFreezeType: gpuFreezeType,
		}

		ctx := context.WithValue(cmd.Context(), keys.DUMP_REQ_CONTEXT_KEY, req)
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

		for _, message := range resp.GetMessages() {
			fmt.Println(message)
		}

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

		return nil
	},
}

var jobDumpCmd = &cobra.Command{
	Use:               "job <JID>",
	Short:             "Dump a managed process/container (job)",
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

		return nil
	},
}

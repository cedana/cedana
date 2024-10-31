package cmd

// By default, implements only the process dump subcommand.
// Other subcommands, for e.g. runc, are imported from installed plugins, as they could
// declare their own flags and subcommands. For runc, see plugins/runc/cmd/dump.go.

import (
	"context"
	"fmt"
	"strconv"

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
	dumpCmd.AddCommand(processDumpCmd)
	dumpCmd.AddCommand(jobDumpCmd)

	// Add common flags
	dumpCmd.PersistentFlags().StringP(types.DirFlag.Full, types.DirFlag.Short, "", "directory to dump to")
	dumpCmd.PersistentFlags().BoolP(types.StreamFlag.Full, types.StreamFlag.Short, false, "stream the dump using cedana-image-streamer")
	dumpCmd.PersistentFlags().BoolP(types.LeaveRunningFlag.Full, types.LeaveRunningFlag.Short, false, "leave the process running after dump")
	dumpCmd.PersistentFlags().BoolP(types.TcpEstablishedFlag.Full, types.TcpEstablishedFlag.Short, false, "dump tcp established connections")

	// Bind to config
	viper.BindPFlag("storage.dump_dir", dumpCmd.PersistentFlags().Lookup(types.DirFlag.Full))
	viper.BindPFlag("criu.leave_running", dumpCmd.PersistentFlags().Lookup(types.LeaveRunningFlag.Full))

	// Add commands from supported plugins
	for name, p := range plugins.LoadedPlugins {
		defer plugins.RecoverFromPanic(name)
		if pluginCmd, err := p.Lookup(plugins.FEATURE_DUMP_CMD); err == nil {
			dumpCmd.AddCommand(*pluginCmd.(**cobra.Command))
		} else {
			log.Debug().Str("plugin", name).Err(err).Msg("Plugin does not provide a dump command")
		}
	}
}

var dumpCmd = &cobra.Command{
	Use:   "dump",
	Short: "Dump a container/process",
	Args:  cobra.ArbitraryArgs,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		dir := viper.GetString("storage.dump_dir")
		leaveRunning := viper.GetBool("criu.leave_running")
		stream, _ := cmd.Flags().GetBool("stream")
		tcpEstablished, _ := cmd.Flags().GetBool("tcp-established")

		// Create half-baked request
		req := &daemon.DumpReq{
			Dir:    dir,
			Stream: stream,
			Details: &daemon.DumpDetails{
				Criu: &daemon.CriuOpts{
					LeaveRunning:   leaveRunning,
					TcpEstablished: tcpEstablished,
				},
			},
		}

		ctx := context.WithValue(cmd.Context(), types.DUMP_REQ_CONTEXT_KEY, req)
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
		req := utils.GetContextValSafe(cmd.Context(), types.DUMP_REQ_CONTEXT_KEY, &daemon.DumpReq{})

		resp, err := client.Dump(cmd.Context(), req)
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
		req := utils.GetContextValSafe(cmd.Context(), types.DUMP_REQ_CONTEXT_KEY, &daemon.DumpReq{})

		if len(args) == 0 {
			return fmt.Errorf("PID is required")
		}
		pid, err := strconv.Atoi(args[0])
		if err != nil {
			return fmt.Errorf("PID must be an number")
		}

		req.Details = &daemon.DumpDetails{
			Type: "process",
			Opts: &daemon.DumpDetails_PID{PID: uint32(pid)},
			Criu: req.GetDetails().GetCriu(),
		}

		ctx := context.WithValue(cmd.Context(), types.DUMP_REQ_CONTEXT_KEY, req)
		cmd.SetContext(ctx)

		return nil
	},
}

var jobDumpCmd = &cobra.Command{
	Use:   "job <JID>",
	Short: "Dump a managed process/container (job)",
	RunE: func(cmd *cobra.Command, args []string) error {
		return nil
	},
}

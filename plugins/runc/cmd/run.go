package cmd

import (
	"context"
	"fmt"
	"os"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/plugins/runc"
	"github.com/cedana/cedana/pkg/keys"
	"github.com/cedana/cedana/pkg/logging"
	"github.com/cedana/cedana/pkg/style"
	runc_flags "github.com/cedana/cedana/plugins/runc/pkg/flags"
	runc_logging "github.com/cedana/cedana/plugins/runc/pkg/logging"
	"github.com/spf13/cobra"
)

func init() {
	RunCmd.Flags().StringP(runc_flags.RootFlag.Full, runc_flags.RootFlag.Short, "", "root")
	RunCmd.Flags().StringP(runc_flags.BundleFlag.Full, runc_flags.BundleFlag.Short, "", "bundle")
	RunCmd.Flags().BoolP(runc_flags.DetachFlag.Full, runc_flags.DetachFlag.Short, false, "detach from the container's process, ignored if not using --no-server and is always true")
	RunCmd.Flags().BoolP(runc_flags.NoPivotFlag.Full, runc_flags.NoPivotFlag.Short, false, "do not use pivot root to jail process inside rootfs.")
	RunCmd.Flags().BoolP(runc_flags.NoNewKeyringFlag.Full, runc_flags.NoNewKeyringFlag.Short, false, "do not create a new session keyring.")
	RunCmd.Flags().StringP(runc_flags.ConsoleSocketFlag.Full, runc_flags.ConsoleSocketFlag.Short, "", "path to an AF_UNIX socket which will receive a file descriptor referencing the master end of the console's pseudoterminal")
	RunCmd.Flags().StringP(runc_flags.LogFlag.Full, runc_flags.LogFlag.Short, "", "log file to write logs to if --no-server")
	RunCmd.Flags().StringP(runc_flags.LogFormatFlag.Full, runc_flags.LogFormatFlag.Short, "text", "log format to use if --no-server (json, text)")
	RunCmd.Flags().StringP(runc_flags.RootlessFlag.Full, runc_flags.RootlessFlag.Short, "auto", "ignore cgroup permission errors (true, false, auto)")
	RunCmd.Flags().BoolP(runc_flags.SystemdCgroupFlag.Full, runc_flags.SystemdCgroupFlag.Short, false, "enable systemd cgroup support, expects cgroupsPath to be of form 'slice:prefix:name' for e.g. 'system.slice:runc:434234'")
	RunCmd.Flags().BoolP(runc_flags.NoSubreaperFlag.Full, runc_flags.NoSubreaperFlag.Short, false, "disable the use of the subreaper used to reap reparented processes")
	RunCmd.Flags().Int32P(runc_flags.PreserveFdsFlag.Full, runc_flags.PreserveFdsFlag.Short, 0, "pass N additional file descriptors to the container (stdio + $LISTEN_FDS + N in total)")
}

var RunCmd = &cobra.Command{
	Use:   "runc [optional-id]",
	Short: "run a runc container",
	Args:  cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		req, ok := cmd.Context().Value(keys.RUN_REQ_CONTEXT_KEY).(*daemon.RunReq)
		if !ok {
			return fmt.Errorf("invalid run request in context")
		}

		var id string
		if len(args) >= 1 {
			id = args[0]
		}

		root, _ := cmd.Flags().GetString(runc_flags.RootFlag.Full)
		bundle, _ := cmd.Flags().GetString(runc_flags.BundleFlag.Full)
		detach, _ := cmd.Flags().GetBool(runc_flags.DetachFlag.Full)
		noPivot, _ := cmd.Flags().GetBool(runc_flags.NoPivotFlag.Full)
		noNewKeyring, _ := cmd.Flags().GetBool(runc_flags.NoNewKeyringFlag.Full)
		consoleSocket, _ := cmd.Flags().GetString(runc_flags.ConsoleSocketFlag.Full)
		logFile, _ := cmd.Flags().GetString(runc_flags.LogFlag.Full)
		logFormat, _ := cmd.Flags().GetString(runc_flags.LogFormatFlag.Full)
		rootless, _ := cmd.Flags().GetString(runc_flags.RootlessFlag.Full)
		systemdCgroup, _ := cmd.Flags().GetBool(runc_flags.SystemdCgroupFlag.Full)
		noSubreaper, _ := cmd.Flags().GetBool(runc_flags.NoSubreaperFlag.Full)
		preserveFds, _ := cmd.Flags().GetInt32(runc_flags.PreserveFdsFlag.Full)
		wd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("Error getting working directory: %v", err)
		}

		daemonless, _ := cmd.Context().Value(keys.DAEMONLESS_CONTEXT_KEY).(bool)
		if daemonless && logFile != "" {
			file, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
			if err != nil {
				return fmt.Errorf("failed to open log file %s: %v", logFile, err)
			}
			logging.SetLogger(runc_logging.Writer(file, logFormat))
		} else if !daemonless {
			if detach {
				fmt.Println(
					style.WarningColors.Sprintf(
						"Flag `%s` is ignored when running with daemon, as it always detaches.",
						runc_flags.DetachFlag.Full,
					))
			}
			if noSubreaper {
				fmt.Println(
					style.WarningColors.Sprintf(
						"Flag `%s` is ignored when running with daemon, as it always detaches and reaps.",
						runc_flags.NoSubreaperFlag.Full,
					))
			}
		}

		req.Type = "runc"
		req.Details = &daemon.Details{Runc: &runc.Runc{
			Root:          root,
			Bundle:        bundle,
			Detach:        detach,
			ID:            id,
			NoPivot:       noPivot,
			NoNewKeyring:  noNewKeyring,
			WorkingDir:    wd,
			ConsoleSocket: consoleSocket,
			Rootless:      rootless,
			SystemdCgroup: systemdCgroup,
			PreserveFDs:   preserveFds,
		}}

		ctx := context.WithValue(cmd.Context(), keys.DUMP_REQ_CONTEXT_KEY, req)
		cmd.SetContext(ctx)

		return nil
	},
}

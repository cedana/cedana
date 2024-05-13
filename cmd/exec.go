package cmd

// This file contains all the exec-related commands when starting `cedana exec ...`

import (
	"fmt"
	"os"

	"github.com/cedana/cedana/api/services"
	"github.com/cedana/cedana/api/services/task"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
	"google.golang.org/grpc/status"
)

var execTaskCmd = &cobra.Command{
	Use:   "exec",
	Short: "Start and register a new process with Cedana",
	Long:  "Start and register a process (cedana exec <task>)",
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) != 1 {
			return fmt.Errorf("requires a task argument")
		}
		return nil
	},
	Run: func(cmd *cobra.Command, args []string) {
		ctx := cmd.Context()
		logger := ctx.Value("logger").(*zerolog.Logger)

		cts, err := services.NewClient()
		if err != nil {
			logger.Error().Err(err).Msg("error creating client")
			return
		}
		defer cts.Close()

		executable := args[0]
		jid, err := cmd.Flags().GetString(idFlag)
		if err != nil {
			logger.Error().Err(err).Msg("invalid job id")
		}

		// Check if executable exists
		if _, err := os.Stat(executable); err != nil {
			logger.Error().Err(err).Msg("executable")
			return
		}

		var env []string
		var uid uint32
		var gid uint32

		asRoot, _ := cmd.Flags().GetBool(rootFlag)
		if !asRoot {
			uid = uint32(os.Getuid())
			gid = uint32(os.Getgid())
		}

		// TODOshould this be gated w/ a flag?
		env = os.Environ()

		wd, _ := cmd.Flags().GetString(wdFlag)
		gpuEnabled, _ := cmd.Flags().GetBool(gpuEnabledFlag)
		taskArgs := &task.StartArgs{
			Task:       executable,
			WorkingDir: wd,
			Env:        env,
			UID:        uid,
			GID:        gid,
			JID:        jid,
			GPU:        gpuEnabled,
		}

		resp, err := cts.Start(ctx, taskArgs)
		if err != nil {
			st, ok := status.FromError(err)
			if ok {
				logger.Error().Err(st.Err()).Msg("start task failed")
			} else {
				logger.Error().Err(err).Msg("start task failed")
			}
			return
		}
		logger.Info().Msgf("Task started: %v", resp)
	},
}

func init() {
	execTaskCmd.Flags().StringP(wdFlag, "w", "", "working directory")
	execTaskCmd.Flags().BoolP(rootFlag, "r", false, "run as root")
	execTaskCmd.Flags().StringP(idFlag, "i", "", "job id to use")
	execTaskCmd.Flags().BoolP(gpuEnabledFlag, "g", false, "enable gpu checkpointing")

	rootCmd.AddCommand(execTaskCmd)
}

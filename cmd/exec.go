package cmd

// This file contains all the exec-related commands when starting `cedana exec ...`

import (
	"fmt"
	"os"

	"github.com/cedana/cedana/api"
	"github.com/cedana/cedana/api/services"
	"github.com/cedana/cedana/api/services/task"
	"github.com/spf13/cobra"
	"google.golang.org/grpc/status"
)

var execTaskCmd = &cobra.Command{
	Use:   "exec",
	Short: "Start and register a new process with Cedana",
	Long:  "Start and register a process by passing a task + id pair (cedana start <task> <id>)",
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) != 2 {
			return fmt.Errorf("requires a task and id argument")
		}
		return nil
	},
	Run: func(cmd *cobra.Command, args []string) {
		cts, err := services.NewClient(api.Address)
		if err != nil {
			logger.Error().Msgf("Error creating client: %v", err)
			return
		}
		defer cts.Close()

		var env []string
		var uid uint32
		var gid uint32
		var taskToExec string = args[0]

		asRoot, _ := cmd.Flags().GetBool(rootFlag)
		if !asRoot {
			uid = uint32(os.Getuid())
			gid = uint32(os.Getgid())
		}

		// TODOshould this be gated w/ a flag?
		env = os.Environ()

		wd, _ := cmd.Flags().GetString(wdFlag)
		taskArgs := &task.StartTaskArgs{
			Task:       taskToExec,
			Id:         args[1],
			WorkingDir: wd,
			Env:        env,
			UID:        uid,
			GID:        gid,
		}

		resp, err := cts.StartTask(taskArgs)
		if err != nil {
			st, ok := status.FromError(err)
			if ok {
				logger.Error().Msgf("Start task failed: %v, %v", st.Message(), st.Code())
			} else {
				logger.Error().Err(err).Msgf("Start task failed")
			}
		} else {
			fmt.Print(resp)
		}
	},
}

func init() {
	execTaskCmd.Flags().StringP(wdFlag, "w", "", "working directory")
	execTaskCmd.Flags().BoolP(rootFlag, "r", false, "run as root")

	rootCmd.AddCommand(execTaskCmd)
}

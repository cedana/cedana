package cmd

// This file contains all the exec-related commands when starting `cedana exec ...`

import (
	"fmt"
	"os"

	"github.com/cedana/cedana/api"
	"github.com/cedana/cedana/api/services"
	"github.com/cedana/cedana/api/services/task"
	"github.com/spf13/cobra"
	bolt "go.etcd.io/bbolt"
	"google.golang.org/grpc/status"
)

var execTaskCmd = &cobra.Command{
	Use:   "exec",
	Short: "Start and register a new process with Cedana",
	Long:  "Start and register a process by passing a task + id pair (cedana exec <task> <id>)",
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

		if _, err := os.Stat(api.DBPath); err == nil {
			conn, err := bolt.Open(api.DBPath, 0600, &bolt.Options{ReadOnly: true})
			if err != nil {
				logger.Fatal().Err(err).Msg("Could not open or create db")
				return
			}
			err = conn.View(func(tx *bolt.Tx) error {
				b := tx.Bucket([]byte("default"))
				c := b.Cursor()
				for k, _ := c.First(); k != nil; k, _ = c.Next() {
					if args[1] == string(k) {
						return fmt.Errorf("A job with this ID is currently running. Please use a unique ID.\n")
					}
				}
				return nil
			})
			if err != nil {
				return
			}
			conn.Close()
		}

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

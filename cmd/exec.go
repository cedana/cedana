package cmd

// This file contains all the exec-related commands when starting `cedana exec ...`

import (
	"bufio"
	"fmt"
	"os"

	"github.com/cedana/cedana/pkg/api/services"
	"github.com/cedana/cedana/pkg/api/services/task"
	"github.com/rs/zerolog/log"
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
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		port, _ := cmd.Flags().GetUint32(portFlag)
		cts, err := services.NewClient(port)
		if err != nil {
			log.Error().Err(err).Msg("error creating client")
			return err
		}
		defer cts.Close()

		executable := args[0]
		jid, err := cmd.Flags().GetString(idFlag)
		if err != nil {
			log.Error().Err(err).Msg("invalid job id")
			return err
		}

		var env []string
		var uid int32
		var gid int32
		var groups []int32 = []int32{}

		asRoot, _ := cmd.Flags().GetBool(rootFlag)
		if !asRoot {
			uid = int32(os.Getuid())
			gid = int32(os.Getgid())
			groups_int, err := os.Getgroups()
			if err != nil {
				log.Error().Err(err).Msg("error getting user groups")
				return err
			}
			for _, g := range groups_int {
				groups = append(groups, int32(g))
			}
		}

		env = os.Environ()

		logRedirectFile, _ := cmd.Flags().GetString(logRedirectFlag)

		wd, _ := cmd.Flags().GetString(wdFlag)
		gpuEnabled, _ := cmd.Flags().GetBool(gpuEnabledFlag)

		attach, _ := cmd.Flags().GetBool(attachFlag)

		taskArgs := &task.StartArgs{
			Task:          executable,
			WorkingDir:    wd,
			Env:           env,
			UID:           uid,
			GID:           gid,
			Groups:        groups,
			JID:           jid,
			GPU:           gpuEnabled,
			LogOutputFile: logRedirectFile,
		}

		if attach {
			stream, err := cts.StartAttach(ctx, &task.StartAttachArgs{Args: taskArgs})
			if err != nil {
				st, ok := status.FromError(err)
				if ok {
					log.Error().Err(st.Err()).Msg("start task failed")
				} else {
					log.Error().Err(err).Msg("start task failed")
				}
			}

			// Handler stdout, stderr
			exitCode := make(chan int)
			go func() {
				for {
					resp, err := stream.Recv()
					if err != nil {
						log.Error().Err(err).Msg("stream ended")
						exitCode <- 1
						return
					}
					if resp.Stdout != "" {
						fmt.Print(resp.Stdout)
					} else if resp.Stderr != "" {
						fmt.Fprint(os.Stderr, resp.Stderr)
					} else {
						exitCode <- int(resp.GetExitCode())
						return
					}
				}
			}()

			// Handle stdin
			go func() {
				scanner := bufio.NewScanner(os.Stdin)
				for scanner.Scan() {
					if err := stream.Send(&task.StartAttachArgs{Stdin: scanner.Text() + "\n"}); err != nil {
						log.Error().Err(err).Msg("error sending stdin")
						return
					}
				}
			}()

			os.Exit(<-exitCode)

			// TODO: Add signal handling properly
		} else {
			resp, err := cts.Start(ctx, taskArgs)
			if err != nil {
				st, ok := status.FromError(err)
				if ok {
					log.Error().Err(st.Err()).Msg("start task failed")
				} else {
					log.Error().Err(err).Msg("start task failed")
				}
				return err
			}
			log.Info().Msgf("Task started: %v", resp)
		}

		return nil
	},
}

func init() {
	execTaskCmd.Flags().StringP(wdFlag, "w", "", "working directory")
	execTaskCmd.Flags().BoolP(rootFlag, "r", false, "run as root")
	execTaskCmd.Flags().StringP(idFlag, "i", "", "job id to use")
	execTaskCmd.Flags().BoolP(gpuEnabledFlag, "g", false, "enable gpu checkpointing")
	execTaskCmd.Flags().StringP(logRedirectFlag, "l", "", "log redirect file (stdout/stderr)")
	execTaskCmd.Flags().BoolP(attachFlag, "a", false, "attach stdin/stdout/stderr")

	rootCmd.AddCommand(execTaskCmd)
}

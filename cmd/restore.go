package cmd

// This file contains all the restore-related commands when starting `cedana restore ...`

import (
	"os"

	"github.com/cedana/cedana/api/services"
	"github.com/cedana/cedana/api/services/task"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"google.golang.org/grpc/status"
)

var restoreCmd = &cobra.Command{
	Use:   "restore",
	Short: "Manually restore a process or container from a checkpoint located at input path: [process, runc (container), containerd (container)]",
}

var restoreProcessCmd = &cobra.Command{
	Use:   "process",
	Short: "Manually restore a process from a checkpoint located at input path",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		ctx := cmd.Context()
		logger := ctx.Value("logger").(*zerolog.Logger)
		cts, err := services.NewClient()
		if err != nil {
			logger.Error().Msgf("Error creating client: %v", err)
			return
		}
		defer cts.Close()

		path := args[0]
		restoreArgs := task.RestoreArgs{
			CheckpointID:   "Not implemented",
			CheckpointPath: path,
		}

		resp, err := cts.Restore(ctx, &restoreArgs)
		if err != nil {
			st, ok := status.FromError(err)
			if ok {
				logger.Error().Msgf("Restore task failed: %v, %v: %v", st.Code(), st.Message(), st.Details())
			} else {
				logger.Error().Msgf("Restore task failed: %v", err)
			}
			return
		}
		logger.Info().Msgf("Response: %v", resp.Message)
	},
}

var restoreJobCmd = &cobra.Command{
	Use:   "job",
	Short: "Manually restore a previously dumped process or container from an input id",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		ctx := cmd.Context()
		logger := ctx.Value("logger").(*zerolog.Logger)
		cts, err := services.NewClient()
		if err != nil {
			logger.Error().Err(err).Msgf("error creating client")
			return
		}
		defer cts.Close()

		// TODO NR: we shouldn't even be reading the db here!!

		var uid uint32
		var gid uint32

		jid := args[0]
		asRoot, _ := cmd.Flags().GetBool(rootFlag)
		if !asRoot {
			uid = uint32(os.Getuid())
			gid = uint32(os.Getgid())
		}

		// Query job state
		query := task.QueryArgs{
			JIDs: []string{jid},
		}

		res, err := cts.Query(ctx, &query)
		if err != nil {
			logger.Error().Msgf("Error querying job: %v", err)
			return
		}
		state := res.Processes[0]

		restoreArgs := task.RestoreArgs{
			JID: jid,
			UID: uid,
			GID: gid,
		}
		if viper.GetBool("remote") {
			remoteState := state.GetRemoteState()
			if remoteState == nil {
				logger.Error().Msgf("No remote state found for id %s", jid)
				return
			}
			// For now just grab latest checkpoint
			if remoteState[len(remoteState)-1].CheckpointID == "" {
				logger.Error().Msgf("No checkpoint found for id %s", jid)
				return
			}
			restoreArgs.CheckpointID = remoteState[len(remoteState)-1].CheckpointID
			restoreArgs.Type = task.CRType_REMOTE
		} else {
			restoreArgs.CheckpointPath = state.GetCheckpointPath()
			restoreArgs.Type = task.CRType_LOCAL
		}

		resp, err := cts.Restore(ctx, &restoreArgs)
		if err != nil {
			st, ok := status.FromError(err)
			if ok {
				logger.Error().Msgf("Restore task failed: %v: %v", st.Code(), st.Message())
			} else {
				logger.Error().Msgf("Restore task failed: %v", err)
			}
			return
		}
		logger.Info().Msgf("Response: %v", resp.Message)
	},
}

var containerdRestoreCmd = &cobra.Command{
	Use:   "containerd",
	Short: "Manually checkpoint a running container to a directory",
	Args:  cobra.ArbitraryArgs,
	Run: func(cmd *cobra.Command, args []string) {
		ctx := cmd.Context()
		logger := ctx.Value("logger").(*zerolog.Logger)
		cts, err := services.NewClient()
		if err != nil {
			logger.Error().Msgf("Error creating client: %v", err)
			return
		}
		defer cts.Close()

		ref, _ := cmd.Flags().GetString(imgFlag)
		id, _ := cmd.Flags().GetString(idFlag)
		restoreArgs := &task.ContainerdRestoreArgs{
			ImgPath:     ref,
			ContainerID: id,
		}

		resp, err := cts.ContainerdRestore(ctx, restoreArgs)
		if err != nil {
			st, ok := status.FromError(err)
			if ok {
				logger.Error().Msgf("Restore task failed: %v, %v", st.Message(), st.Code())
			} else {
				logger.Error().Msgf("Restore task failed: %v", err)
			}
			return
		}
		logger.Info().Msgf("Response: %v", resp.Message)
	},
}

var runcRestoreCmd = &cobra.Command{
	Use:   "runc",
	Short: "Manually restore a running runc container to a directory",
	Args:  cobra.ArbitraryArgs,
	Run: func(cmd *cobra.Command, args []string) {
		ctx := cmd.Context()
		logger := ctx.Value("logger").(*zerolog.Logger)
		cts, err := services.NewClient()
		if err != nil {
			logger.Error().Msgf("Error creating client: %v", err)
			return
		}
		defer cts.Close()

		root, err := cmd.Flags().GetString(rootFlag)
		bundle, err := cmd.Flags().GetString(bundleFlag)
		consoleSocket, err := cmd.Flags().GetString(consoleSocketFlag)
		detach, err := cmd.Flags().GetBool(detachFlag)
		netPid, err := cmd.Flags().GetInt32(netPidFlag)
		opts := &task.RuncOpts{
			Root:          root,
			Bundle:        bundle,
			ConsoleSocket: consoleSocket,
			Detatch:       detach,
			NetPid:        netPid,
		}

		dir, _ := cmd.Flags().GetString(dirFlag)
		id, _ := cmd.Flags().GetString(idFlag)
		isK3s, _ := cmd.Flags().GetBool(isK3sFlag)
		restoreArgs := &task.RuncRestoreArgs{
			ImagePath:   dir,
			ContainerID: id,
			IsK3S:       isK3s,
			Opts:        opts,
			Type:        task.CRType_LOCAL,
			// CheckpointId: checkpointId,
			// FIXME YA: Where does this come from?
		}

		resp, err := cts.RuncRestore(ctx, restoreArgs)
		if err != nil {
			st, ok := status.FromError(err)
			if ok {
				logger.Error().Msgf("Restore task failed: %v, %v", st.Message(), st.Code())
			} else {
				logger.Error().Msgf("Restore task failed: %v", err)
			}
			return
		}
		logger.Info().Msgf("Response: %v", resp.Message)
	},
}

func init() {
	// Process/jobs
	restoreCmd.AddCommand(restoreProcessCmd)
	restoreCmd.AddCommand(restoreJobCmd)
	restoreJobCmd.Flags().BoolP(rootFlag, "r", false, "restore as root")

	// Containerd
	restoreCmd.AddCommand(containerdRestoreCmd)
	containerdRestoreCmd.Flags().StringP(imgFlag, "i", "", "image ref")
	containerdRestoreCmd.MarkFlagRequired(imgFlag)
	containerdRestoreCmd.Flags().StringP(idFlag, "p", "", "container id")
	containerdRestoreCmd.MarkFlagRequired(idFlag)

	// TODO Runc
	restoreCmd.AddCommand(runcRestoreCmd)
	runcRestoreCmd.Flags().StringP(dirFlag, "d", "", "directory to restore from")
	runcRestoreCmd.MarkFlagRequired("dir")
	runcRestoreCmd.Flags().StringP(idFlag, "p", "", "container id")
	runcRestoreCmd.MarkFlagRequired(idFlag)
	runcRestoreCmd.Flags().StringP(bundleFlag, "b", "", "bundle path")
	runcRestoreCmd.MarkFlagRequired(bundleFlag)
	runcRestoreCmd.Flags().StringP(consoleSocketFlag, "c", "", "console socket path")
	runcRestoreCmd.Flags().StringP(rootFlag, "r", RuncRootDir, "runc root directory")
	runcRestoreCmd.Flags().BoolP(detachFlag, "e", false, "run runc container in detached mode")
	runcRestoreCmd.Flags().Bool(isK3sFlag, false, "pass whether or not we are checkpointing a container in a k3s agent")
	runcRestoreCmd.Flags().Int32P(netPidFlag, "n", 0, "provide the network pid to restore to in k3s")

	rootCmd.AddCommand(restoreCmd)
}

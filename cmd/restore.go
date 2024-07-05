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
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		logger := ctx.Value("logger").(*zerolog.Logger)
		cts, err := services.NewClient()
		if err != nil {
			logger.Error().Msgf("Error creating client: %v", err)
			return err
		}
		defer cts.Close()

		var uid uint32
		var gid uint32
		var groups []uint32 = []uint32{}

		asRoot, _ := cmd.Flags().GetBool(rootFlag)
		if !asRoot {
			uid = uint32(os.Getuid())
			gid = uint32(os.Getgid())
			groups_int, err := os.Getgroups()
			if err != nil {
				logger.Error().Err(err).Msg("error getting user groups")
				return err
			}
			for _, g := range groups_int {
				groups = append(groups, uint32(g))
			}
		}

		path := args[0]
		restoreArgs := task.RestoreArgs{
			UID:            uid,
			GID:            gid,
			Groups:         groups,
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
			return err
		}
		logger.Info().Msgf("Response: %v", resp.Message)

		return nil
	},
}

var restoreJobCmd = &cobra.Command{
	Use:   "job",
	Short: "Manually restore a previously dumped process or container from an input id",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		logger := ctx.Value("logger").(*zerolog.Logger)
		cts, err := services.NewClient()
		if err != nil {
			logger.Error().Err(err).Msgf("error creating client")
			return err
		}
		defer cts.Close()

		var uid uint32
		var gid uint32
		var groups []uint32 = []uint32{}

		jid := args[0]
		asRoot, _ := cmd.Flags().GetBool(rootFlag)
		if !asRoot {
			uid = uint32(os.Getuid())
			gid = uint32(os.Getgid())
			groups_int, err := os.Getgroups()
			if err != nil {
				logger.Error().Err(err).Msg("error getting user groups")
				return err
			}
			for _, g := range groups_int {
				groups = append(groups, uint32(g))
			}
		}

		// Query job state
		query := task.QueryArgs{
			JIDs: []string{jid},
		}

		res, err := cts.Query(ctx, &query)
		if err != nil {
			logger.Error().Msgf("Error querying job: %v", err)
			return err
		}
		state := res.Processes[0]

		restoreArgs := task.RestoreArgs{
			JID:    jid,
			UID:    uid,
			GID:    gid,
			Groups: groups,
		}
		if viper.GetBool("remote") {
			remoteState := state.GetRemoteState()
			if remoteState == nil {
				logger.Error().Msgf("No remote state found for id %s", jid)
				return err
			}
			// For now just grab latest checkpoint
			if remoteState[len(remoteState)-1].CheckpointID == "" {
				logger.Error().Msgf("No checkpoint found for id %s", jid)
				return err
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
			return err
		}
		logger.Info().Msgf("Response: %v", resp.Message)

		return nil
	},
}

var containerdRestoreCmd = &cobra.Command{
	Use:   "containerd",
	Short: "Manually checkpoint a running container to a directory",
	Args:  cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		logger := ctx.Value("logger").(*zerolog.Logger)
		cts, err := services.NewClient()
		if err != nil {
			logger.Error().Msgf("Error creating client: %v", err)
			return err
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
			return err
		}
		logger.Info().Msgf("Response: %v", resp.Message)

		return nil
	},
}

var restoreContainerdRootfsCmd = &cobra.Command{
	Use:   "rootfs",
	Short: "Manually restore a container with a checkpointed rootfs",
	Args:  cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		logger := ctx.Value("logger").(*zerolog.Logger)
		cts, err := services.NewClient()
		if err != nil {
			logger.Error().Msgf("Error creating client: %v", err)
			return err
		}
		defer cts.Close()

		ref, _ := cmd.Flags().GetString(refFlag)
		id, _ := cmd.Flags().GetString(idFlag)
		addr, _ := cmd.Flags().GetString(addressFlag)
		ns, _ := cmd.Flags().GetString(namespaceFlag)

		restoreArgs := &task.ContainerdRootfsRestoreArgs{
			ContainerID: id,
			ImageRef:    ref,
			Address:     addr,
			Namespace:   ns,
		}

		resp, err := cts.ContainerdRootfsRestore(ctx, restoreArgs)
		if err != nil {
			st, ok := status.FromError(err)
			if ok {
				logger.Error().Msgf("Restore rootfs container failed: %v, %v", st.Message(), st.Code())
			} else {
				logger.Error().Msgf("Restore rootfs container failed: %v", err)
			}
			return err
		}
		logger.Info().Msgf("Successfully restored rootfs container: %v", resp.ImageRef)

		return nil
	},
}

var runcRestoreCmd = &cobra.Command{
	Use:   "runc",
	Short: "Manually restore a running runc container to a directory",
	Args:  cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		logger := ctx.Value("logger").(*zerolog.Logger)
		cts, err := services.NewClient()
		if err != nil {
			logger.Error().Msgf("Error creating client: %v", err)
			return err
		}
		defer cts.Close()

		root, err := cmd.Flags().GetString(rootFlag)
		if runcRootPath[root] == "" {
			logger.Error().Msgf("container root %s not supported", root)
			return err
		}
		bundle, err := cmd.Flags().GetString(bundleFlag)
		consoleSocket, err := cmd.Flags().GetString(consoleSocketFlag)
		detach, err := cmd.Flags().GetBool(detachFlag)
		netPid, err := cmd.Flags().GetInt32(netPidFlag)
		opts := &task.RuncOpts{
			Root:          runcRootPath[root],
			Bundle:        bundle,
			ConsoleSocket: consoleSocket,
			Detach:        detach,
			NetPid:        netPid,
		}

		dir, _ := cmd.Flags().GetString(dirFlag)
		id, _ := cmd.Flags().GetString(idFlag)
		logger.Log().Msg(id)
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
			return err
		}
		logger.Info().Msgf("Response: %v", resp.Message)

		return nil
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

	restoreCmd.AddCommand(restoreContainerdRootfsCmd)
	restoreContainerdRootfsCmd.Flags().StringP(idFlag, "p", "", "container id")
	restoreContainerdRootfsCmd.MarkFlagRequired(imgFlag)
	restoreContainerdRootfsCmd.Flags().String(refFlag, "", "image ref")
	restoreContainerdRootfsCmd.MarkFlagRequired(refFlag)
	restoreContainerdRootfsCmd.Flags().StringP(addressFlag, "a", "", "containerd sock address")
	restoreContainerdRootfsCmd.MarkFlagRequired(addressFlag)
	restoreContainerdRootfsCmd.Flags().StringP(namespaceFlag, "n", "", "containerd namespace")

	// TODO Runc
	restoreCmd.AddCommand(runcRestoreCmd)
	runcRestoreCmd.Flags().StringP(dirFlag, "d", "", "directory to restore from")
	runcRestoreCmd.MarkFlagRequired("dir")
	runcRestoreCmd.Flags().StringP(idFlag, "p", "", "container id")
	runcRestoreCmd.MarkFlagRequired(idFlag)
	runcRestoreCmd.Flags().StringP(bundleFlag, "b", "", "bundle path")
	runcRestoreCmd.MarkFlagRequired(bundleFlag)
	runcRestoreCmd.Flags().StringP(consoleSocketFlag, "c", "", "console socket path")
	runcRestoreCmd.Flags().StringP(rootFlag, "r", "default", "runc root directory")
	runcRestoreCmd.Flags().BoolP(detachFlag, "e", false, "run runc container in detached mode")
	runcRestoreCmd.Flags().Bool(isK3sFlag, false, "pass whether or not we are checkpointing a container in a k3s agent")
	runcRestoreCmd.Flags().Int32P(netPidFlag, "n", 0, "provide the network pid to restore to in k3s")

	rootCmd.AddCommand(restoreCmd)
}

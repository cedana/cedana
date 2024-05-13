package cmd

// This file contains all the dump-related commands when starting `cedana dump ...`

import (
	"fmt"
	"strconv"

	"github.com/cedana/cedana/api"
	"github.com/cedana/cedana/api/services"
	"github.com/cedana/cedana/api/services/task"
	"github.com/rs/xid"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"google.golang.org/grpc/status"
)

var dumpCmd = &cobra.Command{
	Use:   "dump",
	Short: "Manually checkpoint a process or container to a directory: [process, runc (container), containerd (container)]",
}

var dumpProcessCmd = &cobra.Command{
	Use:   "process",
	Short: "Manually checkpoint a running process [pid] to a directory [-d]",
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

		pid, err := strconv.Atoi(args[0])
		if err != nil {
			logger.Error().Msgf("Error parsing pid: %v", err)
			return
		}

		id := xid.New().String()
		logger.Info().Msgf("no job id specified, using %s", id)

		dir, _ := cmd.Flags().GetString(dirFlag)
		if dir == "" {
			// TODO NR - should we default to /tmp?
			dir = viper.GetString("shared_storage.dump_storage_dir")
			if dir == "" {
				logger.Error().Msgf("no dump directory specified")
				return
			}
			logger.Info().Msgf("no directory specified as input, using %s from config", dir)
		}

		// always self serve when invoked from CLI
		cpuDumpArgs := task.DumpArgs{
			PID:  int32(pid),
			Dir:  dir,
			JID:  id,
			Type: task.CRType_LOCAL,
		}

		resp, err := cts.Dump(ctx, &cpuDumpArgs)
		if err != nil {
			st, ok := status.FromError(err)
			if ok {
				logger.Error().Msgf("Checkpoint task failed: %v, %v: %v", st.Code(), st.Message(), st.Details())
			} else {
				logger.Error().Msgf("Checkpoint task failed: %v", err)
			}
			return
		}
		logger.Info().Msgf("Response: %v", resp.Message)
	},
}

var dumpJobCmd = &cobra.Command{
	Use: "job",
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) != 1 {
			return fmt.Errorf("requires a job id argument, use cedana ps to see available jobs")
		}
		return nil
	},
	Short: "Manually checkpoint a running job to a directory [-d]",
	Run: func(cmd *cobra.Command, args []string) {
		ctx := cmd.Context()
		logger := ctx.Value("logger").(*zerolog.Logger)
		// TODO NR - this needs to be extended to include container checkpoints
		cts, err := services.NewClient()
		if err != nil {
			logger.Error().Msgf("Error creating client: %v", err)
			return
		}
		defer cts.Close()

		id := args[0]

		if id == "" {
			logger.Error().Msgf("no job id specified")
			return
		}

		dir, _ := cmd.Flags().GetString(dirFlag)
		if dir == "" {
			dir = viper.GetString("shared_storage.dump_storage_dir")
			if dir == "" {
				logger.Error().Msgf("no dump directory specified")
				return
			}
			logger.Info().Msgf("no directory specified as input, using %s from config", dir)
		}

		var taskType task.CRType
		if viper.GetBool("remote") {
			taskType = task.CRType_REMOTE
		} else {
			taskType = task.CRType_LOCAL
		}

		gpuEnabled, _ := cmd.Flags().GetBool(gpuEnabledFlag)
		dumpArgs := task.DumpArgs{
			JID:  id,
			Dir:  dir,
			Type: taskType,
			GPU:  gpuEnabled,
		}

		resp, err := cts.Dump(ctx, &dumpArgs)
		if err != nil {
			st, ok := status.FromError(err)
			if ok {
				logger.Error().Msgf("checkpoint task failed: %v: %v", st.Code(), st.Message())
			} else {
				logger.Error().Err(err).Msgf("checkpoint task failed")
			}
			return
		}
		logger.Info().Msgf("Response: %v", resp.Message)
	},
}

var dumpContainerdCmd = &cobra.Command{
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
		dumpArgs := task.ContainerdDumpArgs{
			ContainerID: id,
			Ref:         ref,
		}
		resp, err := cts.ContainerdDump(ctx, &dumpArgs)
		if err != nil {
			st, ok := status.FromError(err)
			if ok {
				logger.Error().Msgf("Checkpoint task failed: %v, %v", st.Message(), st.Code())
			} else {
				logger.Error().Msgf("Checkpoint task failed: %v", err)
			}
			return
		}
		logger.Info().Msgf("Response: %v", resp.Message)
	},
}

var dumpRuncCmd = &cobra.Command{
	Use:   "runc",
	Short: "Manually checkpoint a running runc container to a directory",
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

		rootPath := map[string]string{
			"k8s":    api.K8S_RUNC_ROOT,
			"docker": api.DOCKER_RUNC_ROOT,
		}

		root, _ := cmd.Flags().GetString(containerRootFlag)
		if rootPath[root] == "" {
			logger.Error().Msgf("container root %s not supported", root)
			return
		}

		dir, _ := cmd.Flags().GetString(dirFlag)
		wdPath, _ := cmd.Flags().GetString(wdFlag)
		tcpEstablished, _ := cmd.Flags().GetBool(tcpEstablishedFlag)

		criuOpts := &task.CriuOpts{
			ImagesDirectory: dir,
			WorkDirectory:   wdPath,
			LeaveRunning:    true,
			TcpEstablished:  tcpEstablished,
		}

		id, err := cmd.Flags().GetString(idFlag)
		if err != nil {
			logger.Error().Msgf("Error getting container id: %v", err)
		}

		dumpArgs := task.RuncDumpArgs{
			Root: root,
			// CheckpointPath: checkpointPath,
			// FIXME YA: Where does this come from?
			ContainerID: id,
			CriuOpts:    criuOpts,
			// TODO BS: hard coded for now
			Type: task.CRType_LOCAL,
		}

		resp, err := cts.RuncDump(ctx, &dumpArgs)
		if err != nil {
			st, ok := status.FromError(err)
			if ok {
				logger.Error().Msgf("Checkpoint task failed: %v, %v", st.Message(), st.Code())
			} else {
				logger.Error().Msgf("Checkpoint task failed: %v", err)
			}
			return
		}
		logger.Info().Msgf("Response: %v", resp.Message)
	},
}

func init() {
	// Process/jobs
	dumpCmd.AddCommand(dumpProcessCmd)
	dumpCmd.AddCommand(dumpJobCmd)

	dumpProcessCmd.Flags().StringP(dirFlag, "d", "", "directory to dump to")
	dumpProcessCmd.MarkFlagRequired(dirFlag)
	dumpProcessCmd.Flags().BoolP(gpuEnabledFlag, "g", false, "gpu enabled")

	dumpJobCmd.Flags().StringP(dirFlag, "d", "", "directory to dump to")
	dumpJobCmd.MarkFlagRequired(dirFlag)
	dumpJobCmd.Flags().BoolP(gpuEnabledFlag, "g", false, "gpu enabled")

	// Containerd
	dumpCmd.AddCommand(dumpContainerdCmd)
	dumpContainerdCmd.Flags().StringP(imgFlag, "i", "", "image checkpoint path")
	dumpContainerdCmd.MarkFlagRequired(imgFlag)
	dumpContainerdCmd.Flags().StringP(idFlag, "p", "", "container id")
	dumpContainerdCmd.MarkFlagRequired(idFlag)

	// TODO Runc
	dumpCmd.AddCommand(dumpRuncCmd)
	dumpRuncCmd.Flags().StringP(dirFlag, "d", "", "directory to dump to")
	dumpRuncCmd.MarkFlagRequired(dirFlag)
	dumpRuncCmd.Flags().StringP(idFlag, "i", "", "container id")
	dumpRuncCmd.MarkFlagRequired(idFlag)
	dumpRuncCmd.Flags().BoolP(tcpEstablishedFlag, "t", false, "tcp established")
	dumpRuncCmd.Flags().StringP(containerRootFlag, "r", "k8s", "container root")
	dumpRuncCmd.Flags().BoolP(gpuEnabledFlag, "g", false, "gpu enabled")

	rootCmd.AddCommand(dumpCmd)
}

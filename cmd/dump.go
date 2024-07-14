package cmd

// This file contains all the dump-related commands when starting `cedana dump ...`

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/cedana/cedana/api/services"
	"github.com/cedana/cedana/api/services/task"
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
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		logger := ctx.Value("logger").(*zerolog.Logger)
		cts, err := services.NewClient()
		if err != nil {
			logger.Error().Msgf("Error creating client: %v", err)
			return err
		}
		defer cts.Close()

		pid, err := strconv.Atoi(args[0])
		if err != nil {
			logger.Error().Msgf("Error parsing pid: %v", err)
			return err
		}

		dir, _ := cmd.Flags().GetString(dirFlag)
		if dir == "" {
			// TODO NR - should we default to /tmp?
			dir = viper.GetString("shared_storage.dump_storage_dir")
			if dir == "" {
				logger.Error().Msgf("no dump directory specified")
				return err
			}
			logger.Info().Msgf("no directory specified as input, using %s from config", dir)
		}

		// always self serve when invoked from CLI
		gpuEnabled, _ := cmd.Flags().GetBool(gpuEnabledFlag)
		tcpEstablished, _ := cmd.Flags().GetBool(tcpEstablishedFlag)
		cpuDumpArgs := task.DumpArgs{
			PID:            int32(pid),
			Dir:            dir,
			Type:           task.CRType_LOCAL,
			GPU:            gpuEnabled,
			TcpEstablished: tcpEstablished,
		}

		resp, err := cts.Dump(ctx, &cpuDumpArgs)
		if err != nil {
			st, ok := status.FromError(err)
			if ok {
				logger.Error().Msgf("Checkpoint task failed: %v, %v: %v", st.Code(), st.Message(), st.Details())
			} else {
				logger.Error().Msgf("Checkpoint task failed: %v", err)
			}
			return err
		}
		logger.Info().Msgf("Response: %v", resp.Message)

		return nil
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
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		logger := ctx.Value("logger").(*zerolog.Logger)
		// TODO NR - this needs to be extended to include container checkpoints
		cts, err := services.NewClient()
		if err != nil {
			logger.Error().Msgf("Error creating client: %v", err)
			return err
		}
		defer cts.Close()

		id := args[0]

		if id == "" {
			logger.Error().Msgf("no job id specified")
			return err
		}

		dir, _ := cmd.Flags().GetString(dirFlag)
		if dir == "" {
			dir = viper.GetString("shared_storage.dump_storage_dir")
			if dir == "" {
				logger.Error().Msgf("no dump directory specified")
				return err
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
		tcpEstablished, _ := cmd.Flags().GetBool(tcpEstablishedFlag)
		dumpArgs := task.DumpArgs{
			JID:            id,
			Dir:            dir,
			Type:           taskType,
			GPU:            gpuEnabled,
			TcpEstablished: tcpEstablished,
		}

		resp, err := cts.Dump(ctx, &dumpArgs)
		if err != nil {
			st, ok := status.FromError(err)
			if ok {
				logger.Error().Msgf("checkpoint task failed: %v: %v", st.Code(), st.Message())
			} else {
				logger.Error().Err(err).Msgf("checkpoint task failed")
			}
			return err
		}
		logger.Info().Msgf("Response: %v", resp.Message)

		return nil
	},
}

var dumpContainerdCmd = &cobra.Command{
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
			return err
		}
		logger.Info().Msgf("Response: %v", resp.Message)

		return nil
	},
}

var dumpContainerdRootfsCmd = &cobra.Command{
	Use:   "rootfs",
	Short: "Manually checkpoint a running runc container's rootfs and bundle into an image",
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

		id, err := cmd.Flags().GetString(idFlag)
		ref, err := cmd.Flags().GetString(refFlag)
		addr, err := cmd.Flags().GetString(addressFlag)
		ns, err := cmd.Flags().GetString(namespaceFlag)

		// Default to moby if ns is not provided
		if ns == "" {
			ns = "moby"
		}

		dumpArgs := task.ContainerdRootfsDumpArgs{
			ContainerID: id,
			ImageRef:    ref,
			Address:     addr,
			Namespace:   ns,
		}

		resp, err := cts.ContainerdRootfsDump(ctx, &dumpArgs)
		if err != nil {
			st, ok := status.FromError(err)
			if ok {
				logger.Error().Msgf("Checkpoint rootfs failed: %v, %v", st.Message(), st.Code())
			} else {
				logger.Error().Msgf("Checkpoint rootfs failed: %v", err)
			}
			return err
		}
		logger.Info().Msgf("Saved rootfs and stored in new image: %s", resp.ImageRef)

		return nil
	},
}

var dumpRuncCmd = &cobra.Command{
	Use:   "runc",
	Short: "Manually checkpoint a running runc container to a directory",
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

		root, _ := cmd.Flags().GetString(rootFlag)
		if runcRootPath[root] == "" {
			logger.Error().Msgf("container root %s not supported", root)
			return err
		}

		dir, _ := cmd.Flags().GetString(dirFlag)
		wdPath, _ := cmd.Flags().GetString(wdFlag)
		tcpEstablished, _ := cmd.Flags().GetBool(tcpEstablishedFlag)
		pid, _ := cmd.Flags().GetInt(pidFlag)

		external, _ := cmd.Flags().GetString(externalFlag)

		var externalNamespaces []string

		namespaces := strings.Split(external, ",")
		if external != "" {
			for _, ns := range namespaces {
				nsParts := strings.Split(ns, ":")

				nsType := nsParts[0]
				nsDestination := nsParts[1]

				externalNamespaces = append(externalNamespaces, fmt.Sprintf("%s[%s]:extRootPidNS", nsType, nsDestination))
			}
		}

		criuOpts := &task.CriuOpts{
			ImagesDirectory: dir,
			WorkDirectory:   wdPath,
			LeaveRunning:    true,
			TcpEstablished:  tcpEstablished,
			External:        externalNamespaces,
		}

		id, err := cmd.Flags().GetString(idFlag)
		if err != nil {
			logger.Error().Msgf("Error getting container id: %v", err)
		}

		dumpArgs := task.RuncDumpArgs{
			Root: runcRootPath[root],
			// CheckpointPath: checkpointPath,
			// FIXME YA: Where does this come from?
			Pid:         int32(pid),
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
			return err
		}
		logger.Info().Msgf("Response: %v", resp.Message)

		return nil
	},
}

func init() {
	// Process/jobs
	dumpCmd.AddCommand(dumpProcessCmd)
	dumpCmd.AddCommand(dumpJobCmd)

	dumpProcessCmd.Flags().StringP(dirFlag, "d", "", "directory to dump to")
	dumpProcessCmd.MarkFlagRequired(dirFlag)
	dumpProcessCmd.Flags().BoolP(gpuEnabledFlag, "g", false, "checkpoint gpu")
	dumpProcessCmd.Flags().BoolP(tcpEstablishedFlag, "t", false, "tcp established")

	dumpJobCmd.Flags().StringP(dirFlag, "d", "", "directory to dump to")
	dumpJobCmd.MarkFlagRequired(dirFlag)
	dumpJobCmd.Flags().BoolP(gpuEnabledFlag, "g", false, "checkpoint gpu")
	dumpJobCmd.Flags().BoolP(tcpEstablishedFlag, "t", false, "tcp established")

	// Containerd
	dumpCmd.AddCommand(dumpContainerdCmd)
	dumpContainerdCmd.Flags().StringP(imgFlag, "i", "", "image checkpoint path")
	dumpContainerdCmd.MarkFlagRequired(imgFlag)
	dumpContainerdCmd.Flags().StringP(idFlag, "p", "", "container id")
	dumpContainerdCmd.MarkFlagRequired(idFlag)

	dumpContainerdRootfsCmd.Flags().StringP(idFlag, "p", "", "container id")
	dumpContainerdRootfsCmd.MarkFlagRequired(imgFlag)
	dumpContainerdRootfsCmd.Flags().String(refFlag, "", "image ref")
	dumpContainerdRootfsCmd.MarkFlagRequired(refFlag)
	dumpContainerdRootfsCmd.Flags().StringP(addressFlag, "a", "", "containerd sock address")
	dumpContainerdRootfsCmd.MarkFlagRequired(addressFlag)
	dumpContainerdRootfsCmd.Flags().StringP(namespaceFlag, "n", "", "containerd namespace")

	// TODO Runc
	dumpCmd.AddCommand(dumpRuncCmd)
	dumpRuncCmd.Flags().StringP(dirFlag, "d", "", "directory to dump to")
	dumpRuncCmd.MarkFlagRequired(dirFlag)
	dumpRuncCmd.Flags().StringP(idFlag, "i", "", "container id")
	dumpRuncCmd.MarkFlagRequired(idFlag)
	dumpRuncCmd.Flags().BoolP(tcpEstablishedFlag, "t", false, "tcp established")
	dumpRuncCmd.Flags().StringP(rootFlag, "r", "default", "container root")
	dumpRuncCmd.Flags().BoolP(gpuEnabledFlag, "g", false, "gpu enabled")
	dumpRuncCmd.Flags().IntP(pidFlag, "p", 0, "pid")
	dumpRuncCmd.Flags().String(externalFlag, "", "external")

	dumpCmd.AddCommand(dumpCRIORootfs)
	dumpCRIORootfs.Flags().StringP(idFlag, "i", "", "container id")
	dumpCRIORootfs.MarkFlagRequired(idFlag)
	dumpCRIORootfs.Flags().StringP(destFlag, "d", "", "directory to dump to")
	dumpCRIORootfs.MarkFlagRequired(destFlag)
	dumpCRIORootfs.Flags().StringP(containerStorageFlag, "s", "", "crio container storage location")
	dumpCRIORootfs.MarkFlagRequired(containerStorageFlag)

	dumpCmd.AddCommand(dumpContainerdRootfsCmd)

	rootCmd.AddCommand(dumpCmd)

	rootCmd.AddCommand(pushCRIOImage)
	pushCRIOImage.Flags().StringP(refFlag, "", "", "original ref")
	pushCRIOImage.MarkFlagRequired(refFlag)
	pushCRIOImage.Flags().StringP(newRefFlag, "", "", "directory to dump to")
	pushCRIOImage.MarkFlagRequired(newRefFlag)
	pushCRIOImage.Flags().StringP(rootfsDiffPathFlag, "r", "", "crio container storage location")
	pushCRIOImage.MarkFlagRequired(rootfsDiffPathFlag)
}

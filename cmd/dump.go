package cmd

// This file contains all the dump-related commands when starting `cedana dump ...`

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	task "buf.build/gen/go/cedana/task/protocolbuffers/go"
	"github.com/cedana/cedana/pkg/api/services"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/rs/xid"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"google.golang.org/grpc/status"
)

var dumpCmd = &cobra.Command{
	Use:   "dump",
	Short: "Manually checkpoint a process or container to a directory: [process, runc (container), containerd (container)]",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		port, _ := cmd.Flags().GetUint32(portFlag)
		cts, err := services.NewClient(port)
		if err != nil {
			return fmt.Errorf("Error creating client: %v", err)
		}
		ctx := context.WithValue(cmd.Context(), utils.CtsKey, cts)
		cmd.SetContext(ctx)
		return nil
	},
	PersistentPostRun: func(cmd *cobra.Command, args []string) {
		cts := cmd.Context().Value(utils.CtsKey).(*services.ServiceClient)
		cts.Close()
	},
}

var dumpProcessCmd = &cobra.Command{
	Use:   "process",
	Short: "Manually checkpoint a running process [pid] to a directory [-d]",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		cts := cmd.Context().Value(utils.CtsKey).(*services.ServiceClient)

		pid, err := strconv.Atoi(args[0])
		if err != nil {
			log.Error().Msgf("Error parsing pid: %v", err)
			return err
		}

		dir, _ := cmd.Flags().GetString(dirFlag)
		tcpEstablished, _ := cmd.Flags().GetBool(tcpEstablishedFlag)
		tcpClose, _ := cmd.Flags().GetBool(tcpCloseFlag)
		tcpSkipInFlight, _ := cmd.Flags().GetBool(skipInFlightFlag)
		leaveRunning, _ := cmd.Flags().GetBool(leaveRunningFlag)
		stream, _ := cmd.Flags().GetInt32(streamFlag)

		log.Info().Msgf("cmd/dump stream = %d", stream)
		if stream > 0 {
			if _, err := exec.LookPath("cedana-image-streamer"); err != nil {
				log.Error().Msgf("Cannot find cedana-image-streamer in PATH")
				return err
			}
		}
		cpuDumpArgs := task.DumpArgs{
			PID:    int32(pid),
			Dir:    dir,
			Stream: stream,
			CriuOpts: &task.CriuOpts{
				LeaveRunning:    leaveRunning,
				TcpEstablished:  tcpEstablished,
				TcpClose:        tcpClose,
				TcpSkipInFlight: tcpSkipInFlight,
			},
		}
		resp, err := cts.Dump(ctx, &cpuDumpArgs)
		if err != nil {
			st, ok := status.FromError(err)
			if ok {
				log.Error().Str("message", st.Message()).Str("code", st.Code().String()).Msgf("Failed")
			} else {
				log.Error().Err(err).Msgf("Failed")
			}
			return err
		}
		log.Info().Str("message", resp.Message).Interface("stats", resp.DumpStats).Str("Checkpoint", resp.CheckpointID).Msgf("Success")

		return nil
	},
}

var dumpKataCmd = &cobra.Command{
	Use:   "kata",
	Short: "Manually checkpoint a running workload in the kata-vm [vm-name] to a directory [-d]",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		vm := args[0]

		port, _ := cmd.Flags().GetUint32(portFlag)
		cts, err := services.NewClient(port)
		if err != nil {
			log.Error().Msgf("Error creating client: %v", err)
			return err
		}
		defer cts.Close()

		id := xid.New().String()
		log.Info().Msgf("no job id specified, using %s", id)

		dir, _ := cmd.Flags().GetString(dirFlag)
		vmSnapshot, _ := cmd.Flags().GetBool(vmSnapshotFlag)
		vmSocketPath, _ := cmd.Flags().GetString(vmSocketPathFlag)

		dumpArgs := &task.HostDumpKataArgs{
			VmName:       vm,
			Dir:          dir,
			Port:         1024,
			VMSnapshot:   vmSnapshot,
			VMSocketPath: vmSocketPath,
		}

		resp, err := cts.HostKataDump(ctx, dumpArgs)
		if err != nil {
			st, ok := status.FromError(err)
			if ok {
				log.Error().Msgf("Checkpoint task failed: %v, %v: %v", st.Code(), st.Message(), st.Details())
			} else {
				log.Error().Msgf("Checkpoint task failed: %v", err)
			}
			return err
		}
		log.Info().Msgf("Dump dir: %v", resp.TarDumpDir)

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
		cts := cmd.Context().Value(utils.CtsKey).(*services.ServiceClient)

		id := args[0]
		if id == "" {
			log.Error().Msgf("no job id specified")
			return fmt.Errorf("no job id specified")
		}

		dir, _ := cmd.Flags().GetString(dirFlag)
		tcpEstablished, _ := cmd.Flags().GetBool(tcpEstablishedFlag)
		tcpClose, _ := cmd.Flags().GetBool(tcpCloseFlag)
		tcpSkipInFlight, _ := cmd.Flags().GetBool(skipInFlightFlag)
		leaveRunning, _ := cmd.Flags().GetBool(leaveRunningFlag)
		fileLocks, _ := cmd.Flags().GetBool(fileLocksFlag)
		external, _ := cmd.Flags().GetString(externalFlag)
		stream, _ := cmd.Flags().GetInt32(streamFlag)
		if stream > 0 {
			if _, err := exec.LookPath("cedana-image-streamer"); err != nil {
				log.Error().Msgf("Cannot find cedana-image-streamer in PATH")
				return err
			}
		}

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

		dumpArgs := task.JobDumpArgs{
			JID:    id,
			Dir:    dir,
			Stream: stream,
			CriuOpts: &task.CriuOpts{
				LeaveRunning:    leaveRunning,
				TcpEstablished:  tcpEstablished,
				External:        externalNamespaces,
				FileLocks:       fileLocks,
				TcpClose:        tcpClose,
				TcpSkipInFlight: tcpSkipInFlight,
			},
		}

		resp, err := cts.JobDump(ctx, &dumpArgs)
		if err != nil {
			st, ok := status.FromError(err)
			if ok {
				log.Error().Str("message", st.Message()).Str("code", st.Code().String()).Msgf("Failed")
			} else {
				log.Error().Err(err).Msgf("Failed")
			}
			return err
		}
		log.Info().Str("message", resp.Message).Interface("stats", resp.DumpStats).Str("Checkpoint", resp.CheckpointID).Msgf("Success")

		return nil
	},
}

var dumpContainerdCmd = &cobra.Command{
	Use:   "containerd",
	Short: "Manually checkpoint a running container to a directory",
	Args:  cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		cts := cmd.Context().Value(utils.CtsKey).(*services.ServiceClient)

		ref, _ := cmd.Flags().GetString(refFlag)
		id, _ := cmd.Flags().GetString(idFlag)
		address, _ := cmd.Flags().GetString(addressFlag)
		namespace, _ := cmd.Flags().GetString(namespaceFlag)

		rootfsArgs := task.ContainerdRootfsDumpArgs{
			ContainerID: id,
			ImageRef:    ref,
			Address:     address,
			Namespace:   namespace,
		}

		root, _ := cmd.Flags().GetString(rootFlag)
		dir, _ := cmd.Flags().GetString(dirFlag)
		wdPath, _ := cmd.Flags().GetString(wdFlag)
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
			External:        externalNamespaces,
		}

		runcArgs := task.RuncDumpArgs{
			Root:        getRuncRootPath(root),
			ContainerID: id,
			CriuOpts:    criuOpts,
			// TODO BS: hard coded for now
		}

		// TODO BS missing runc dump args
		dumpArgs := task.ContainerdDumpArgs{
			ContainerdRootfsDumpArgs: &rootfsArgs,
			RuncDumpArgs:             &runcArgs,
		}

		_, err := cts.ContainerdDump(ctx, &dumpArgs)
		if err != nil {
			st, ok := status.FromError(err)
			if ok {
				log.Error().Msgf("Checkpoint task failed: %v, %v", st.Message(), st.Code())
			} else {
				log.Error().Msgf("Checkpoint task failed: %v", err)
			}
			return err
		}
		log.Info().Msgf("success")

		return nil
	},
}

var dumpContainerdRootfsCmd = &cobra.Command{
	Use:   "rootfs",
	Short: "Manually checkpoint a running runc container's rootfs and bundle into an image",
	Args:  cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		cts := cmd.Context().Value(utils.CtsKey).(*services.ServiceClient)

		id, err := cmd.Flags().GetString(idFlag)
		if err != nil {
			log.Warn().Err(err).Send()
		}
		ref, err := cmd.Flags().GetString(refFlag)
		if err != nil {
			log.Warn().Err(err).Send()
		}
		addr, err := cmd.Flags().GetString(addressFlag)
		if err != nil {
			log.Warn().Err(err).Send()
		}
		ns, err := cmd.Flags().GetString(namespaceFlag)
		if err != nil {
			log.Warn().Err(err).Send()
		}

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
				log.Error().Msgf("Checkpoint rootfs failed: %v, %v", st.Message(), st.Code())
			} else {
				log.Error().Msgf("Checkpoint rootfs failed: %v", err)
			}
			return err
		}
		log.Info().Msgf("Saved rootfs and stored in new image: %s", resp.ImageRef)

		return nil
	},
}

var dumpRuncCmd = &cobra.Command{
	Use:   "runc",
	Short: "Manually checkpoint a running runc container to a directory",
	Args:  cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		cts := cmd.Context().Value(utils.CtsKey).(*services.ServiceClient)

		root, _ := cmd.Flags().GetString(rootFlag)
		dir, _ := cmd.Flags().GetString(dirFlag)
		wdPath, _ := cmd.Flags().GetString(wdFlag)
		tcpEstablished, _ := cmd.Flags().GetBool(tcpEstablishedFlag)
		tcpClose, _ := cmd.Flags().GetBool(tcpCloseFlag)
		tcpSkipInFlight, _ := cmd.Flags().GetBool(skipInFlightFlag)
		leaveRunning, _ := cmd.Flags().GetBool(leaveRunningFlag)
		fileLocks, _ := cmd.Flags().GetBool(fileLocksFlag)
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
			WorkDirectory:   wdPath,
			LeaveRunning:    leaveRunning,
			TcpEstablished:  tcpEstablished,
			TcpClose:        tcpClose,
			TcpSkipInFlight: tcpSkipInFlight,
			External:        externalNamespaces,
			FileLocks:       fileLocks,
		}

		id, err := cmd.Flags().GetString(idFlag)
		if err != nil {
			log.Error().Msgf("Error getting container id: %v", err)
		}

		dumpArgs := task.RuncDumpArgs{
			Dir:         dir,
			Root:        getRuncRootPath(root),
			ContainerID: id,
			CriuOpts:    criuOpts,
		}

		resp, err := cts.RuncDump(ctx, &dumpArgs)
		if err != nil {
			st, ok := status.FromError(err)
			if ok {
				log.Error().Str("message", st.Message()).Str("code", st.Code().String()).Msgf("Failed")
			} else {
				log.Error().Err(err).Msgf("Failed")
			}
			return err
		}
		log.Info().Str("message", resp.Message).Interface("stats", resp.DumpStats).Str("Checkpoint", resp.CheckpointID).Msgf("Success")

		return nil
	},
}

var dumpCRIORootfs = &cobra.Command{
	Use:   "crioRootfs",
	Short: "Manually commit a CRIO container",
	Args:  cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		cts := cmd.Context().Value(utils.CtsKey).(*services.ServiceClient)

		id, err := cmd.Flags().GetString(idFlag)
		if err != nil {
			log.Error().Msgf("Error getting container id: %v", err)
		}
		dest, err := cmd.Flags().GetString(destFlag)
		if err != nil {
			log.Error().Msgf("Error getting destination path: %v", err)
		}
		containerStorage, err := cmd.Flags().GetString(containerStorageFlag)
		if err != nil {
			log.Error().Msgf("Error getting container storage path: %v", err)
		}

		dumpArgs := task.CRIORootfsDumpArgs{
			ContainerID:      id,
			Dest:             dest,
			ContainerStorage: containerStorage,
		}

		resp, err := cts.CRIORootfsDump(ctx, &dumpArgs)
		if err != nil {
			st, ok := status.FromError(err)
			if ok {
				log.Error().Msgf("Checkpoint task failed: %v, %v", st.Message(), st.Code())
			} else {
				log.Error().Msgf("Checkpoint task failed: %v", err)
			}
			return err
		}
		log.Info().Msgf("Response: %v", resp)

		return nil
	},
}

func init() {
	// Process
	dumpCmd.AddCommand(dumpProcessCmd)
	dumpProcessCmd.Flags().StringP(dirFlag, "d", "", "directory to dump to")
	dumpProcessCmd.Flags().BoolP(tcpEstablishedFlag, "t", false, "tcp established")
	dumpProcessCmd.Flags().BoolP(tcpCloseFlag, "", false, "tcp close")
	dumpProcessCmd.Flags().Int32P(streamFlag, "s", 0, "dump images using criu-image-streamer")
	dumpProcessCmd.Flags().Bool(leaveRunningFlag, false, "leave running")
	dumpProcessCmd.Flags().Bool(skipInFlightFlag, false, "skip in-flight TCP connections")

	// Job
	dumpCmd.AddCommand(dumpJobCmd)
	dumpJobCmd.Flags().StringP(dirFlag, "d", "", "directory to dump to")
	dumpJobCmd.Flags().BoolP(tcpEstablishedFlag, "t", false, "tcp established")
	dumpJobCmd.Flags().BoolP(tcpCloseFlag, "", false, "tcp close")
	dumpJobCmd.Flags().Int32P(streamFlag, "s", 0, "dump images using criu-image-streamer")
	dumpJobCmd.Flags().Bool(leaveRunningFlag, false, "leave running")
	dumpJobCmd.Flags().Bool(fileLocksFlag, false, "dump file locks")
	dumpJobCmd.Flags().StringP(externalFlag, "e", "", "external namespaces")
	dumpJobCmd.Flags().Bool(skipInFlightFlag, false, "skip in-flight TCP connections")

	// Kata
	dumpCmd.AddCommand(dumpKataCmd)
	dumpKataCmd.Flags().StringP(dirFlag, "d", "", "directory to dump to")
	dumpKataCmd.MarkFlagRequired(dirFlag)
	dumpKataCmd.Flags().Bool(vmSnapshotFlag, false, "is vmsnapshot")
	dumpKataCmd.Flags().Uint32P(portFlag, "p", DEFAULT_PORT, "port for cts client")
	dumpKataCmd.Flags().String(vmSocketPathFlag, "", "socket path for full vm snapshot")

	// Containerd
	dumpCmd.AddCommand(dumpContainerdCmd)
	dumpContainerdCmd.Flags().String(idFlag, "", "container id")
	dumpContainerdCmd.Flags().String(refFlag, "", "image ref")
	dumpContainerdCmd.MarkFlagRequired(refFlag)
	dumpContainerdCmd.Flags().StringP(addressFlag, "a", "", "containerd sock address")
	dumpContainerdCmd.MarkFlagRequired(addressFlag)
	dumpContainerdCmd.Flags().StringP(namespaceFlag, "n", "", "containerd namespace")
	dumpContainerdCmd.Flags().StringP(dirFlag, "d", "", "directory to dump to")
	dumpContainerdCmd.Flags().StringP(rootFlag, "r", "default", "container root")
	dumpContainerdCmd.Flags().StringP(externalFlag, "e", "", "external namespaces")

	// Containerd Rootfs
	dumpCmd.AddCommand(dumpContainerdRootfsCmd)
	dumpContainerdRootfsCmd.Flags().StringP(idFlag, "i", "", "container id")
	dumpContainerdRootfsCmd.MarkFlagRequired(imgFlag)
	dumpContainerdRootfsCmd.Flags().String(refFlag, "", "image ref")
	dumpContainerdRootfsCmd.MarkFlagRequired(refFlag)
	dumpContainerdRootfsCmd.Flags().StringP(addressFlag, "a", "", "containerd sock address")
	dumpContainerdRootfsCmd.MarkFlagRequired(addressFlag)
	dumpContainerdRootfsCmd.Flags().StringP(namespaceFlag, "n", "", "containerd namespace")

	// Runc
	dumpCmd.AddCommand(dumpRuncCmd)
	dumpRuncCmd.Flags().StringP(dirFlag, "d", "", "directory to dump to")
	dumpRuncCmd.Flags().StringP(idFlag, "i", "", "container id")
	dumpRuncCmd.MarkFlagRequired(idFlag)
	dumpRuncCmd.Flags().BoolP(tcpEstablishedFlag, "t", false, "tcp established")
	dumpRuncCmd.Flags().BoolP(tcpCloseFlag, "", false, "tcp close")
	dumpRuncCmd.Flags().StringP(wdFlag, "w", "", "working directory")
	dumpRuncCmd.Flags().StringP(rootFlag, "r", "default", "container root")
	dumpRuncCmd.Flags().String(externalFlag, "", "external")
	dumpRuncCmd.Flags().Bool(leaveRunningFlag, false, "leave running")
	dumpRuncCmd.Flags().Bool(fileLocksFlag, false, "dump file locks")
	dumpRuncCmd.Flags().Bool(skipInFlightFlag, false, "skip in-flight TCP connections")

	// CRIO
	dumpCmd.AddCommand(dumpCRIORootfs)
	dumpCRIORootfs.Flags().StringP(idFlag, "i", "", "container id")
	dumpCRIORootfs.MarkFlagRequired(idFlag)
	dumpCRIORootfs.Flags().StringP(destFlag, "d", "", "directory to dump to")
	dumpCRIORootfs.MarkFlagRequired(destFlag)
	dumpCRIORootfs.Flags().StringP(containerStorageFlag, "s", "", "crio container storage location")
	dumpCRIORootfs.MarkFlagRequired(containerStorageFlag)

	rootCmd.AddCommand(dumpCmd)
}

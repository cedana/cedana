package cmd

// This file contains all the dump-related commands when starting `cedana dump ...`

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/cedana/cedana/pkg/api/services"
	"github.com/cedana/cedana/pkg/api/services/task"
	"github.com/rs/xid"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"google.golang.org/grpc/status"

	"github.com/mdlayher/vsock"
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

		port, _ := cmd.Flags().GetUint32(portFlag)
		cts, err := services.NewClient(port)
		if err != nil {
			log.Error().Msgf("Error creating client: %v", err)
			return err
		}
		defer cts.Close()

		pid, err := strconv.Atoi(args[0])
		if err != nil {
			log.Error().Msgf("Error parsing pid: %v", err)
			return err
		}

		dir, _ := cmd.Flags().GetString(dirFlag)

		// always self serve when invoked from CLI
		gpuEnabled, _ := cmd.Flags().GetBool(gpuEnabledFlag)
		tcpEstablished, _ := cmd.Flags().GetBool(tcpEstablishedFlag)
		leaveRunning, _ := cmd.Flags().GetBool(leaveRunningFlag)
		stream, _ := cmd.Flags().GetBool(streamFlag)
		cpuDumpArgs := task.DumpArgs{
			PID:    int32(pid),
			Dir:    dir,
			Type:   task.CRType_LOCAL,
			GPU:    gpuEnabled,
			Stream: stream,
			CriuOpts: &task.CriuOpts{
				LeaveRunning:   leaveRunning,
				TcpEstablished: tcpEstablished,
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
		log.Info().Str("message", resp.Message).Interface("stats", resp.DumpStats).Msgf("Success")

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
		cts, err := services.NewVSockClient(vm, port)
		if err != nil {
			log.Error().Msgf("Error creating client: %v", err)
			return err
		}
		defer cts.Close()

		id := xid.New().String()
		log.Info().Msgf("no job id specified, using %s", id)

		dir, _ := cmd.Flags().GetString(dirFlag)

		cpuDumpArgs := task.DumpArgs{
			Dir:  "/tmp",
			JID:  id,
			Type: task.CRType_LOCAL,
		}

		go func() {
			listener, err := vsock.Listen(9999, nil)
			if err != nil {
				return
			}
			defer listener.Close()

			conn, err := listener.Accept()
			if err != nil {
				return
			}
			defer conn.Close()

			// Open the file for writing
			file, err := os.Create(dir + "/dmp.tar")
			if err != nil {
				return
			}
			defer file.Close()

			buffer := make([]byte, 1024)

			// Receive data and write to file
			for {
				bytesReceived, err := conn.Read(buffer)
				if err != nil {
					if err == io.EOF {
						break
					}
					return
				}

				_, err = file.Write(buffer[:bytesReceived])
				if err != nil {
					return
				}
			}
		}()

		resp, err := cts.KataDump(ctx, &cpuDumpArgs)
		if err != nil {
			st, ok := status.FromError(err)
			if ok {
				log.Error().Msgf("Checkpoint task failed: %v, %v: %v", st.Code(), st.Message(), st.Details())
			} else {
				log.Error().Msgf("Checkpoint task failed: %v", err)
			}
			return err
		}
		log.Info().Msgf("Response: %v", resp.Message)

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

		port, _ := cmd.Flags().GetUint32(portFlag)
		cts, err := services.NewClient(port)
		if err != nil {
			log.Error().Msgf("Error creating client: %v", err)
			return err
		}
		defer cts.Close()

		id := args[0]

		if id == "" {
			log.Error().Msgf("no job id specified")
			return err
		}

		dir, _ := cmd.Flags().GetString(dirFlag)

		gpuEnabled, _ := cmd.Flags().GetBool(gpuEnabledFlag)
		tcpEstablished, _ := cmd.Flags().GetBool(tcpEstablishedFlag)
		leaveRunning, _ := cmd.Flags().GetBool(leaveRunningFlag)
		stream, _ := cmd.Flags().GetBool(streamFlag)
		if stream {
			if _, err := exec.LookPath("cedana-image-streamer"); err != nil {
				log.Error().Msgf("Cannot find cedana-image-streamer in PATH")
				return err
			}
		}
		dumpArgs := task.DumpArgs{
			JID:    id,
			Dir:    dir,
			GPU:    gpuEnabled,
			Stream: stream,
			CriuOpts: &task.CriuOpts{
				LeaveRunning:   leaveRunning,
				TcpEstablished: tcpEstablished,
			},
		}

		resp, err := cts.Dump(ctx, &dumpArgs)
		if err != nil {
			st, ok := status.FromError(err)
			if ok {
				log.Error().Str("message", st.Message()).Str("code", st.Code().String()).Msgf("Failed")
			} else {
				log.Error().Err(err).Msgf("Failed")
			}
			return err
		}
		log.Info().Str("message", resp.Message).Interface("stats", resp.DumpStats).Msgf("Success")

		return nil
	},
}

var dumpContainerdCmd = &cobra.Command{
	Use:   "containerd",
	Short: "Manually checkpoint a running container to a directory",
	Args:  cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		port, _ := cmd.Flags().GetUint32(portFlag)
		cts, err := services.NewClient(port)
		if err != nil {
			log.Error().Msgf("Error creating client: %v", err)
			return err
		}
		defer cts.Close()

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
			External:        externalNamespaces,
		}

		runcArgs := task.RuncDumpArgs{
			Root: root,
			// CheckpointPath: checkpointPath,
			// FIXME YA: Where does this come from?
			Pid:         int32(pid),
			ContainerID: id,
			CriuOpts:    criuOpts,
			// TODO BS: hard coded for now
			Type: task.CRType_LOCAL,
		}

		// TODO BS missing runc dump args
		dumpArgs := task.ContainerdDumpArgs{
			ContainerdRootfsDumpArgs: &rootfsArgs,
			RuncDumpArgs:             &runcArgs,
		}

		_, err = cts.ContainerdDump(ctx, &dumpArgs)
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

		port, _ := cmd.Flags().GetUint32(portFlag)
		cts, err := services.NewClient(port)
		if err != nil {
			log.Error().Msgf("Error creating client: %v", err)
			return err
		}
		defer cts.Close()

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

		port, _ := cmd.Flags().GetUint32(portFlag)
		cts, err := services.NewClient(port)
		if err != nil {
			log.Error().Msgf("Error creating client: %v", err)
			return err
		}
		defer cts.Close()

		root, _ := cmd.Flags().GetString(rootFlag)
		if runcRootPath[root] == "" {
			log.Error().Msgf("container root %s not supported", root)
			return err
		}

		dir, _ := cmd.Flags().GetString(dirFlag)
		wdPath, _ := cmd.Flags().GetString(wdFlag)
		tcpEstablished, _ := cmd.Flags().GetBool(tcpEstablishedFlag)
		pid, _ := cmd.Flags().GetInt(pidFlag)
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
			ImagesDirectory: dir,
			WorkDirectory:   wdPath,
			LeaveRunning:    leaveRunning,
			TcpEstablished:  tcpEstablished,
			External:        externalNamespaces,
			FileLocks:       fileLocks,
		}

		id, err := cmd.Flags().GetString(idFlag)
		if err != nil {
			log.Error().Msgf("Error getting container id: %v", err)
		}

		dumpArgs := task.RuncDumpArgs{
			Root:        runcRootPath[root],
			Pid:         int32(pid),
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
		log.Info().Str("message", resp.Message).Interface("stats", resp.DumpStats).Msgf("Success")

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
	dumpProcessCmd.Flags().BoolP(streamFlag, "s", false, "dump images using criu-image-streamer")
	dumpProcessCmd.Flags().Bool(leaveRunningFlag, false, "leave running")

	dumpJobCmd.Flags().StringP(dirFlag, "d", "", "directory to dump to")
	dumpJobCmd.MarkFlagRequired(dirFlag)
	dumpJobCmd.Flags().BoolP(gpuEnabledFlag, "g", false, "checkpoint gpu")
	dumpJobCmd.Flags().BoolP(tcpEstablishedFlag, "t", false, "tcp established")
	dumpJobCmd.Flags().BoolP(streamFlag, "s", false, "dump images using criu-image-streamer")
	dumpJobCmd.Flags().Bool(leaveRunningFlag, false, "leave running")

	// Kata
	dumpCmd.AddCommand(dumpKataCmd)
	dumpKataCmd.Flags().StringP(dirFlag, "d", "", "directory to dump to")
	dumpKataCmd.MarkFlagRequired(dirFlag)

	// Containerd
	// ref, _ := cmd.Flags().GetString(imgFlag)
	// id, _ := cmd.Flags().GetString(idFlag)
	// address, _ := cmd.Flags().GetString(addressFlag)
	// namespace, _ := cmd.Flags().GetString(namespaceFlag)

	// Runc
	// dir, _ := cmd.Flags().GetString(dirFlag)
	// wdPath, _ := cmd.Flags().GetString(wdFlag)
	// pid, _ := cmd.Flags().GetInt(pidFlag)
	// external, _ := cmd.Flags().GetString(externalFlag)

	dumpCmd.AddCommand(dumpContainerdCmd)
	dumpContainerdCmd.Flags().String(idFlag, "", "container id")
	dumpContainerdCmd.Flags().String(refFlag, "", "image ref")
	dumpContainerdCmd.MarkFlagRequired(refFlag)
	dumpContainerdCmd.Flags().StringP(addressFlag, "a", "", "containerd sock address")
	dumpContainerdCmd.MarkFlagRequired(addressFlag)
	dumpContainerdCmd.Flags().StringP(namespaceFlag, "n", "", "containerd namespace")

	dumpContainerdCmd.Flags().StringP(dirFlag, "d", "", "directory to dump to")
	dumpContainerdCmd.MarkFlagRequired(dirFlag)
	dumpContainerdCmd.Flags().StringP(rootFlag, "r", "default", "container root")
	dumpContainerdCmd.Flags().BoolP(gpuEnabledFlag, "g", false, "gpu enabled")
	dumpContainerdCmd.Flags().Int(pidFlag, 0, "pid")
	dumpContainerdCmd.Flags().String(externalFlag, "", "external")

	dumpContainerdRootfsCmd.Flags().StringP(idFlag, "i", "", "container id")
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
	dumpRuncCmd.Flags().Int(pidFlag, 0, "pid")
	dumpRuncCmd.Flags().String(externalFlag, "", "external")
	dumpRuncCmd.Flags().Bool(leaveRunningFlag, false, "leave running")
	dumpRuncCmd.Flags().Bool(fileLocksFlag, false, "dump file locks")

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

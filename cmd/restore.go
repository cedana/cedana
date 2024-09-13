package cmd

// This file contains all the restore-related commands when starting `cedana restore ...`

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/cedana/cedana/pkg/api"
	"github.com/cedana/cedana/pkg/api/services"
	"github.com/cedana/cedana/pkg/api/services/task"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"google.golang.org/grpc/status"

	"github.com/cedana/cedana/pkg/utils"
	"github.com/mdlayher/vsock"
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
		port, _ := cmd.Flags().GetUint32(portFlag)
		cts, err := services.NewClient(port)
		if err != nil {
			log.Error().Msgf("Error creating client: %v", err)
			return err
		}
		defer cts.Close()

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

		path := args[0]
		tcpEstablished, _ := cmd.Flags().GetBool(tcpEstablishedFlag)
		stream, _ := cmd.Flags().GetBool(streamFlag)
		restoreArgs := task.RestoreArgs{
			UID:            uid,
			GID:            gid,
			Groups:         groups,
			CheckpointID:   "Not implemented",
			CheckpointPath: path,
			Stream:         stream,
			CriuOpts: &task.CriuOpts{
				TcpEstablished: tcpEstablished,
			},
		}

		resp, err := cts.Restore(ctx, &restoreArgs)
		if err != nil {
			st, ok := status.FromError(err)
			if ok {
				log.Error().Str("message", st.Message()).Str("code", st.Code().String()).Msgf("Failed")
			} else {
				log.Error().Err(err).Msgf("Failed")
			}
			return err
		}
		log.Info().Str("message", resp.Message).Int32("PID", resp.NewPID).Interface("stats", resp.RestoreStats).Msgf("Success")

		return nil
	},
}

var restoreKataCmd = &cobra.Command{
	Use:   "kata",
	Short: "Manually restore a workload in the kata-vm [vm-name] from a directory [-d]",
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

		path, _ := cmd.Flags().GetString(dirFlag)
		tcpEstablished, _ := cmd.Flags().GetBool(tcpEstablishedFlag)
		restoreArgs := task.RestoreArgs{
			CheckpointID:   vm,
			CheckpointPath: "/tmp/dmp.tar",
			CriuOpts: &task.CriuOpts{
				TcpEstablished: tcpEstablished,
			},
		}

		go func() {
			time.Sleep(1 * time.Second)

			// extract cid from the process tree on host
			cid, err := utils.ExtractCID(vm)
			if err != nil {
				return
			}

			conn, err := vsock.Dial(cid, api.KATA_TAR_FILE_RECEIVER_PORT, nil)
			if err != nil {
				return
			}
			defer conn.Close()

			// Open the file
			file, err := os.Open(path)
			if err != nil {
				return
			}
			defer file.Close()

			buffer := make([]byte, 1024)

			// Read from file and send over VSOCK connection
			for {
				bytesRead, err := file.Read(buffer)
				if err != nil {
					if err == io.EOF {
						break
					}
					return
				}

				_, err = conn.Write(buffer[:bytesRead])
				if err != nil {
					return
				}
			}
		}()

		resp, err := cts.KataRestore(ctx, &restoreArgs)
		if err != nil {
			st, ok := status.FromError(err)
			if ok {
				log.Error().Msgf("Restore task failed: %v, %v: %v", st.Code(), st.Message(), st.Details())
			} else {
				log.Error().Msgf("Restore task failed: %v", err)
			}
			return err
		}
		log.Info().Msgf("Response: %v", resp.Message)

		return nil
	},
}

var restoreJobCmd = &cobra.Command{
	Use:   "job",
	Short: "Manually restore a previously dumped process or container from an input id",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		port, _ := cmd.Flags().GetUint32(portFlag)
		cts, err := services.NewClient(port)
		if err != nil {
			log.Error().Err(err).Msgf("error creating client")
			return err
		}
		defer cts.Close()

		var uid int32
		var gid int32
		var groups []int32 = []int32{}

		jid := args[0]
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

		tcpEstablished, _ := cmd.Flags().GetBool(tcpEstablishedFlag)
		stream, _ := cmd.Flags().GetBool(streamFlag)
		restoreArgs := task.RestoreArgs{
			JID:    jid,
			UID:    uid,
			GID:    gid,
			Groups: groups,
			Stream: stream,
			CriuOpts: &task.CriuOpts{
				TcpEstablished: tcpEstablished,
			},
		}

		attach, _ := cmd.Flags().GetBool(attachFlag)
		if attach {
			stream, err := cts.RestoreAttach(ctx, &task.RestoreAttachArgs{Args: &restoreArgs})
			if err != nil {
				st, ok := status.FromError(err)
				if ok {
					log.Error().Err(st.Err()).Msg("restore failed")
				} else {
					log.Error().Err(err).Msg("restore failed")
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
					if err := stream.Send(&task.RestoreAttachArgs{Stdin: scanner.Text() + "\n"}); err != nil {
						log.Error().Err(err).Msg("error sending stdin")
						return
					}
				}
			}()

			os.Exit(<-exitCode)

			// TODO: Add signal handling properly
		}

		resp, err := cts.Restore(ctx, &restoreArgs)
		if err != nil {
			st, ok := status.FromError(err)
			if ok {
				log.Error().Str("message", st.Message()).Str("code", st.Code().String()).Msgf("Failed")
			} else {
				log.Error().Err(err).Msgf("Failed")
			}
			return err
		}
		log.Info().Str("message", resp.Message).Int32("PID", resp.NewPID).Interface("stats", resp.RestoreStats).Msgf("Success")

		return nil
	},
}

var containerdRestoreCmd = &cobra.Command{
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
				log.Error().Msgf("Restore task failed: %v, %v", st.Message(), st.Code())
			} else {
				log.Error().Msgf("Restore task failed: %v", err)
			}
			return err
		}
		log.Info().Msgf("Response: %v", resp.Message)

		return nil
	},
}

var runcRestoreCmd = &cobra.Command{
	Use:   "runc",
	Short: "Manually restore a running runc container to a directory",
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

		root, err := cmd.Flags().GetString(rootFlag)
		if runcRootPath[root] == "" {
			log.Error().Msgf("container root %s not supported", root)
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
		fileLocks, _ := cmd.Flags().GetBool(fileLocksFlag)
		restoreArgs := &task.RuncRestoreArgs{
			ImagePath:   dir,
			ContainerID: id,
			Opts:        opts,
			Type:        task.CRType_LOCAL,
			CriuOpts: &task.CriuOpts{
				FileLocks: fileLocks,
			},
		}

		resp, err := cts.RuncRestore(ctx, restoreArgs)
		if err != nil {
			st, ok := status.FromError(err)
			if ok {
				log.Error().Str("message", st.Message()).Str("code", st.Code().String()).Msgf("Failed")
			} else {
				log.Error().Err(err).Msgf("Failed")
			}
			return err
		}
		log.Info().Str("message", resp.Message).Interface("stats", resp.RestoreStats).Msgf("Success")

		return nil
	},
}

func init() {
	// Process/jobs
	restoreCmd.AddCommand(restoreProcessCmd)
	restoreCmd.AddCommand(restoreJobCmd)

	restoreProcessCmd.Flags().BoolP(tcpEstablishedFlag, "t", false, "restore with TCP connections established")
	restoreProcessCmd.Flags().BoolP(streamFlag, "s", false, "restore images using criu-image-streamer")
	restoreJobCmd.Flags().BoolP(tcpEstablishedFlag, "t", false, "restore with TCP connections established")
	restoreJobCmd.Flags().BoolP(streamFlag, "s", false, "restore images using criu-image-streamer")
	restoreJobCmd.Flags().BoolP(rootFlag, "r", false, "restore as root")
	restoreJobCmd.Flags().BoolP(attachFlag, "a", false, "attach stdin/stdout/stderr")

	// Kata
	restoreCmd.AddCommand(restoreKataCmd)
	restoreKataCmd.Flags().StringP(dirFlag, "d", "", "path of tar file (inside VM) to restore from")
	restoreKataCmd.MarkFlagRequired(dirFlag)

	// Containerd
	restoreCmd.AddCommand(containerdRestoreCmd)
	containerdRestoreCmd.Flags().String(imgFlag, "", "image ref")
	containerdRestoreCmd.MarkFlagRequired(imgFlag)
	containerdRestoreCmd.Flags().StringP(idFlag, "i", "", "container id")
	containerdRestoreCmd.MarkFlagRequired(idFlag)

	// TODO Runc
	restoreCmd.AddCommand(runcRestoreCmd)
	runcRestoreCmd.Flags().StringP(dirFlag, "d", "", "directory to restore from")
	runcRestoreCmd.MarkFlagRequired("dir")
	runcRestoreCmd.Flags().StringP(idFlag, "i", "", "container id")
	runcRestoreCmd.MarkFlagRequired(idFlag)
	runcRestoreCmd.Flags().StringP(bundleFlag, "b", "", "bundle path")
	runcRestoreCmd.MarkFlagRequired(bundleFlag)
	runcRestoreCmd.Flags().StringP(consoleSocketFlag, "c", "", "console socket path")
	runcRestoreCmd.Flags().StringP(rootFlag, "r", "default", "runc root directory")
	runcRestoreCmd.Flags().BoolP(detachFlag, "e", false, "run runc container in detached mode")
	runcRestoreCmd.Flags().Bool(isK3sFlag, false, "pass whether or not we are checkpointing a container in a k3s agent")
	runcRestoreCmd.Flags().Int32P(netPidFlag, "n", 0, "provide the network pid to restore to in k3s")
	runcRestoreCmd.Flags().Bool(fileLocksFlag, false, "restore file locks")

	rootCmd.AddCommand(restoreCmd)
}

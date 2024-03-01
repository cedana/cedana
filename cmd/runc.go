package cmd

import (
	"github.com/cedana/cedana/api/services/task"
	"github.com/spf13/cobra"
	"google.golang.org/grpc/status"
)

var containerName string
var checkpointId string

var runcRoot = &cobra.Command{
	Use:   "runc",
	Short: "Runc related commands such as ps, get runc id by container name (k8s), etc.",
}

var runcGetRuncIdByName = &cobra.Command{
	Use:   "get",
	Short: "",
	Args:  cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		cli, err := NewCLI()
		if err != nil {
			return err
		}

		runcArgs := &task.CtrByNameArgs{
			Root:          root,
			ContainerName: containerName,
		}

		resp, err := cli.cts.GetRuncIdByName(runcArgs)
		if err != nil {
			return err
		}

		cli.logger.Info().Msgf("Response: %v", resp)

		cli.cts.Close()

		return nil
	},
}

// -----------------------
// Checkpoint/Restore of a runc container
// -----------------------

var runcDumpCmd = &cobra.Command{
	Use:   "runc",
	Short: "Manually checkpoint a running runc container to a directory",
	Args:  cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		cli, err := NewCLI()
		if err != nil {
			return err
		}

		root = "/run/containerd/runc/k8s.io"

		criuOpts := &task.CriuOpts{
			ImagesDirectory: dir,
			WorkDirectory:   workPath,
			LeaveRunning:    true,
			TcpEstablished:  tcpEstablished,
		}

		dumpArgs := task.RuncDumpArgs{
			Root:           root,
			CheckpointPath: checkpointPath,
			ContainerId:    containerId,
			CriuOpts:       criuOpts,
			//TODO BS: hard coded for now
			Type: task.RuncDumpArgs_REMOTE,
		}

		resp, err := cli.cts.CheckpointRunc(&dumpArgs)

		if err != nil {
			st, ok := status.FromError(err)
			if ok {
				cli.logger.Error().Msgf("Checkpoint task failed: %v, %v", st.Message(), st.Code())
			} else {
				cli.logger.Error().Msgf("Checkpoint task failed: %v", err)
			}
		}

		cli.logger.Info().Msgf("Response: %v", resp.Message)

		cli.cts.Close()

		return nil
	},
}
var runcRestoreCmd = &cobra.Command{
	Use:   "runc",
	Short: "Manually restore a running runc container to a directory",
	Args:  cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		cli, err := NewCLI()
		if err != nil {
			return err
		}

		opts := &task.RuncOpts{
			Root:          root,
			Bundle:        bundle,
			ConsoleSocket: consoleSocket,
			Detatch:       detach,
			NetPid:        netPid,
		}

		restoreArgs := &task.RuncRestoreArgs{
			ImagePath:    dir,
			ContainerId:  containerId,
			IsK3S:        isK3s,
			Opts:         opts,
			Type:         task.RuncRestoreArgs_LOCAL,
			CheckpointId: checkpointId,
		}

		resp, err := cli.cts.RuncRestore(restoreArgs)
		if err != nil {
			st, ok := status.FromError(err)
			if ok {
				cli.logger.Error().Msgf("Restore task failed: %v, %v", st.Message(), st.Code())
			} else {
				cli.logger.Error().Msgf("Restore task failed: %v", err)
			}
		}

		cli.logger.Info().Msgf("Response: %v", resp.Message)

		cli.cts.Close()

		return nil
	},
}

func initRuncCommands() {
	runcRestoreCmd.Flags().StringVarP(&dir, "dir", "d", "", "directory to restore from")
	runcRestoreCmd.MarkFlagRequired("dir")
	runcRestoreCmd.Flags().StringVarP(&containerId, "id", "p", "", "container id")
	runcRestoreCmd.MarkFlagRequired("id")
	runcRestoreCmd.Flags().StringVarP(&bundle, "bundle", "b", "", "bundle path")
	runcRestoreCmd.MarkFlagRequired("bundle")
	runcRestoreCmd.Flags().StringVarP(&consoleSocket, "console-socket", "c", "", "console socket path")
	runcRestoreCmd.Flags().StringVarP(&root, "root", "r", "/var/run/runc", "runc root directory")
	runcRestoreCmd.Flags().BoolVarP(&detach, "detach", "e", false, "run runc container in detached mode")
	runcRestoreCmd.Flags().BoolVar(&isK3s, "isK3s", false, "pass whether or not we are checkpointing a container in a k3s agent")
	runcRestoreCmd.Flags().Int32VarP(&netPid, "netPid", "n", 0, "provide the network pid to restore to in k3s")

	restoreCmd.AddCommand(runcRestoreCmd)

	runcDumpCmd.Flags().StringVarP(&dir, "dir", "d", "", "directory to dump to")
	runcDumpCmd.MarkFlagRequired("dir")
	runcDumpCmd.Flags().StringVarP(&containerId, "id", "i", "", "container id")
	runcDumpCmd.MarkFlagRequired("id")
	runcDumpCmd.Flags().BoolVarP(&tcpEstablished, "tcp-established", "t", false, "tcp established")

	dumpCmd.AddCommand(runcDumpCmd)

	runcGetRuncIdByName.Flags().StringVarP(&root, "root", "r", "/var/run/runc", "runc root directory")
	runcGetRuncIdByName.Flags().StringVarP(&containerName, "container-name", "c", "", "name of container in k8s")
	runcRoot.AddCommand(runcGetRuncIdByName)
}

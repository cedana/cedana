package cmd

import (
	"fmt"
	"net/rpc"
	"os"
	"strconv"

	"github.com/cedana/cedana/api"
	"github.com/cedana/cedana/container"
	"github.com/cedana/cedana/utils"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
)

var dir string
var ref string
var containerId string
var imgPath string
var runcPath string
var runcPid int32
var workPath string

type CLI struct {
	cfg    *utils.Config
	conn   *rpc.Client
	logger zerolog.Logger
}

func NewCLI() (*CLI, error) {
	cfg, err := utils.InitConfig()
	if err != nil {
		return nil, err
	}
	client, err := rpc.Dial("unix", "/tmp/cedana.sock")
	if err != nil {
		return nil, fmt.Errorf("could not connect to daemon at /tmp/cedana.sock, running as root?: %w", err)
	}
	logger := utils.GetLogger()

	return &CLI{
		cfg:    cfg,
		conn:   client,
		logger: logger,
	}, nil
}

var dumpCmd = &cobra.Command{
	Use:   "dump",
	Short: "Manually checkpoint a running process to a directory",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cli, err := NewCLI()
		if err != nil {
			return err
		}

		pid, err := strconv.Atoi(args[0])
		if err != nil {
			return err
		}

		if dir == "" {
			if cli.cfg.SharedStorage.DumpStorageDir == "" {
				return fmt.Errorf("no dump directory specified")
			}
			dir = cli.cfg.SharedStorage.DumpStorageDir
			cli.logger.Info().Msgf("no directory specified as input, defaulting to %s", dir)
		}

		a := api.DumpArgs{
			PID: int32(pid),
			Dir: dir,
		}

		var resp api.DumpResp
		err = cli.conn.Call("CedanaDaemon.Dump", a, &resp)
		if err != nil {
			return err
		}

		cli.logger.Info().Msgf("checkpoint of process %d written successfully to %s", pid, dir)
		return nil
	},
}

var containerdDumpCmd = &cobra.Command{
	Use:   "containerd",
	Short: "Manually checkpoint a running container to a directory",
	Args:  cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		cli, err := NewCLI()
		if err != nil {
			return err
		}

		a := api.ContainerDumpArgs{
			Ref:         ref,
			ContainerId: containerId,
		}

		var resp api.ContainerDumpResp
		err = cli.conn.Call("CedanaDaemon.ContainerDump", a, &resp)
		if err != nil {
			return err
		}
		cli.logger.Info().Msgf("container %s dumped successfully to %s", containerId, dir)
		return nil
	},
}

var runcDumpCmd = &cobra.Command{
	Use:   "runc",
	Short: "Manually checkpoint a running runc container to a directory",
	Args:  cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		cli, err := NewCLI()
		if err != nil {
			return err
		}

		criuOpts := &container.CriuOpts{
			ImagesDirectory: runcPath,
			WorkDirectory:   workPath,
			LeaveRunning:    true,
			TcpEstablished:  false,
		}

		a := api.RuncDumpArgs{
			RuncPath:    runcPath,
			ContainerId: containerId,
			CriuOpts:    *criuOpts,
		}

		var resp api.ContainerDumpResp
		err = cli.conn.Call("CedanaDaemon.RuncDump", a, &resp)
		if err != nil {
			return err
		}
		cli.logger.Info().Msgf("container %s dumped successfully to %s", containerId, runcPath)
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

		opts := &container.RuncOpts{
			Root:          "/var/run/runc",
			Bundle:        "/home/brandonsmith/bundle",
			ConsoleSocket: "/home/brandonsmith/tty.sock",
		}

		a := api.RuncRestoreArgs{
			ImagePath:   runcPath,
			ContainerId: containerId,
			Opts:        opts,
		}

		var resp api.ContainerDumpResp
		err = cli.conn.Call("CedanaDaemon.RuncRestore", a, &resp)
		if err != nil {
			return err
		}
		cli.logger.Info().Msgf("container %s dumped successfully to %s", containerId, dir)
		return nil
	},
}

var restoreCmd = &cobra.Command{
	Use:   "restore",
	Short: "Manually restore a process from a checkpoint located at input path",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cli, err := NewCLI()
		if err != nil {
			return err
		}

		a := api.RestoreArgs{
			Path: args[0],
		}

		var resp api.RestoreResp
		err = cli.conn.Call("CedanaDaemon.Restore", a, &resp)
		if err != nil {
			return err
		}

		return nil
	},
}

var containerdRestoreCmd = &cobra.Command{
	Use:   "containerd",
	Short: "Manually checkpoint a running container to a directory",
	Args:  cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		cli, err := NewCLI()
		if err != nil {
			return err
		}

		a := api.ContainerRestoreArgs{
			ImgPath:     imgPath,
			ContainerId: containerId,
		}

		var resp api.ContainerRestoreResp
		err = cli.conn.Call("CedanaDaemon.ContainerRestore", a, &resp)
		if err != nil {
			return err
		}

		cli.logger.Info().Msgf("container %s restored from %s successfully", containerId, ref)
		return nil
	},
}

var startTaskCmd = &cobra.Command{
	Use:   "start",
	Short: "Start and register a new process with Cedana",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cli, err := NewCLI()
		if err != nil {
			return err
		}

		a := api.StartTaskArgs{
			Task: args[0],
		}

		var resp api.StartTaskResp
		err = cli.conn.Call("CedanaDaemon.StartTask", a, &resp)
		if err != nil {
			return err
		}

		return nil
	},
}

var natsCmd = &cobra.Command{
	Use:   "nats",
	Short: "Start NATS server for cedana client",
	RunE: func(cmd *cobra.Command, args []string) error {
		cli, err := NewCLI()
		if err != nil {
			return err
		}

		selfId, exists := os.LookupEnv("CEDANA_CLIENT_ID")
		if !exists {
			cli.logger.Fatal().Msg("Could not find CEDANA_CLIENT_ID - something went wrong during instance creation")
		}

		jobId, exists := os.LookupEnv("CEDANA_JOB_ID")
		if !exists {
			cli.logger.Fatal().Msg("Could not find CEDANA_JOB_ID - something went wrong during instance creation")
		}

		authToken, exists := os.LookupEnv("CEDANA_AUTH_TOKEN")
		if !exists {
			cli.logger.Fatal().Msg("Could not find CEDANA_AUTH_TOKEN - something went wrong during instance creation")
		}

		a := api.StartNATSArgs{
			SelfID:    selfId,
			JobID:     jobId,
			AuthToken: authToken,
		}

		var resp api.StartNATSResp
		err = cli.conn.Call("CedanaDaemon.StartNATS", a, &resp)
		if err != nil {
			return err
		}

		cli.logger.Info().Msgf("NATS client started, waiting for commands sent to job %s", jobId)

		return nil
	},
}

func initRuncCommands() {
	runcRestoreCmd.Flags().StringVarP(&runcPath, "image", "i", "", "image path")
	runcRestoreCmd.Flags().StringVarP(&containerId, "id", "p", "", "container id")
	restoreCmd.AddCommand(runcRestoreCmd)

	runcDumpCmd.Flags().StringVarP(&runcPath, "image", "i", "", "image path")
	runcDumpCmd.Flags().StringVarP(&containerId, "id", "p", "", "container id")
	dumpCmd.AddCommand(runcDumpCmd)
}
func initContainerdCommands() {
	containerdDumpCmd.Flags().StringVarP(&ref, "image", "i", "", "image checkpoint path")
	containerdDumpCmd.Flags().StringVarP(&containerId, "id", "p", "", "container id")

	dumpCmd.AddCommand(containerdDumpCmd)

	containerdRestoreCmd.Flags().StringVarP(&ref, "image", "i", "", "image ref")
	containerdRestoreCmd.Flags().StringVarP(&containerId, "id", "p", "", "container id")

	restoreCmd.AddCommand(containerdRestoreCmd)
}

func init() {
	dumpCmd.Flags().StringVarP(&dir, "dir", "d", "", "directory to dump to")
	rootCmd.AddCommand(dumpCmd)
	rootCmd.AddCommand(restoreCmd)
	rootCmd.AddCommand(startTaskCmd)

	initRuncCommands()

	initContainerdCommands()

	clientDaemonCmd.AddCommand(natsCmd)
}

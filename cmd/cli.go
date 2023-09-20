package cmd

import (
	"fmt"
	"net/rpc"
	"os"
	"strconv"

	"github.com/cedana/cedana/api"
	"github.com/cedana/cedana/utils"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
)

var dir string
var ref string
var containerId string
var imgPath string

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

var containerDumpCmd = &cobra.Command{
	Use:   "container",
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

var containerRestoreCmd = &cobra.Command{
	Use:   "container",
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

func init() {
	rootCmd.AddCommand(dumpCmd)
	dumpCmd.AddCommand(containerDumpCmd)

	containerDumpCmd.Flags().StringVarP(&ref, "image", "i", "", "image ref")
	containerDumpCmd.Flags().StringVarP(&containerId, "id", "p", "", "container id")

	restoreCmd.AddCommand(containerRestoreCmd)

	containerRestoreCmd.Flags().StringVarP(&ref, "image", "i", "", "image ref")
	containerRestoreCmd.Flags().StringVarP(&containerId, "id", "p", "", "container id")

	rootCmd.AddCommand(restoreCmd)
	rootCmd.AddCommand(startTaskCmd)
	clientDaemonCmd.AddCommand(natsCmd)
	dumpCmd.Flags().StringVarP(&dir, "dir", "d", "", "directory to dump to")
}

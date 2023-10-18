package cmd

import (
	"context"
	"fmt"
	"log"
	"strconv"

	"github.com/cedana/cedana/api/services/task"
	"github.com/cedana/cedana/utils"
	"github.com/rs/xid"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	bolt "go.etcd.io/bbolt"
)

var id string
var dir string
var ref string

var containerId string
var imgPath string
var runcPath string
var root string
var checkpointPath string
var workPath string

var bundle string
var consoleSocket string
var detach bool

type CLI struct {
	cfg    *utils.Config
	cts    *CheckpointTaskService
	logger zerolog.Logger
}

type CheckpointTaskService struct {
	ctx     context.Context
	client  task.TaskServiceClient
	conn    *grpc.ClientConn // Keep a reference to the connection
	address string
}

func NewCheckpointTaskService(addr string) *CheckpointTaskService {
	var opts []grpc.DialOption
	opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	conn, err := grpc.Dial(addr, opts...)
	if err != nil {
		log.Fatalf("fail to dial: %v", err)
	}

	client := task.NewTaskServiceClient(conn)

	ctx := context.Background()

	return &CheckpointTaskService{
		client:  client,
		conn:    conn, // Keep a reference to the connection
		address: addr,
		ctx:     ctx,
	}
}

func (c *CheckpointTaskService) CheckpointTask(args *task.DumpArgs) *task.DumpResp {
	resp, err := c.client.Dump(c.ctx, args)
	if err != nil {
		log.Fatalf("fail to dial: %v", err)
	}
	return resp
}

func (c *CheckpointTaskService) RestoreTask(args *task.RestoreArgs) *task.RestoreResp {
	resp, err := c.client.Restore(c.ctx, args)
	if err != nil {
		log.Fatalf("fail to dial: %v", err)
	}
	return resp
}

func (c *CheckpointTaskService) CheckpointContainer(args *task.ContainerDumpArgs) *task.ContainerDumpResp {
	resp, err := c.client.ContainerDump(c.ctx, args)
	if err != nil {
		log.Fatalf("fail to dial: %v", err)
	}
	return resp
}

func (c *CheckpointTaskService) ContainerRestore(args *task.ContainerRestoreArgs) *task.ContainerRestoreResp {
	resp, err := c.client.ContainerRestore(c.ctx, args)
	if err != nil {
		log.Fatalf("fail to dial: %v", err)
	}
	return resp
}

func (c *CheckpointTaskService) CheckpointRunc(args *task.RuncDumpArgs) *task.RuncDumpResp {
	resp, err := c.client.RuncDump(c.ctx, args)
	if err != nil {
		log.Fatalf("fail to dial: %v", err)
	}
	return resp
}

func (c *CheckpointTaskService) RuncRestore(args *task.RuncRestoreArgs) *task.RuncRestoreResp {
	resp, err := c.client.RuncRestore(c.ctx, args)
	if err != nil {
		log.Fatalf("fail to dial: %v", err)
	}
	return resp
}

func (c *CheckpointTaskService) StartTask(args *task.StartTaskArgs) *task.StartTaskResp {
	resp, err := c.client.StartTask(c.ctx, args)
	if err != nil {
		log.Fatalf("fail to dial: %v", err)
	}
	return resp
}

func (c *CheckpointTaskService) Close() {
	c.conn.Close()
}

func NewCLI() (*CLI, error) {
	cfg, err := utils.InitConfig()
	if err != nil {
		return nil, err
	}
	cts := NewCheckpointTaskService("localhost:8080")

	logger := utils.GetLogger()

	return &CLI{
		cfg:    cfg,
		cts:    cts,
		logger: logger,
	}, nil
}

var dumpCmd = &cobra.Command{
	Use:   "dump",
	Short: "Manually checkpoint a process or container to a directory: [process, runc (container), containerd (container)]",
}

var dumpProcessCmd = &cobra.Command{
	Use:   "process",
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

		if id == "" {
			id = xid.New().String()
			cli.logger.Info().Msgf("no id specified, defaulting to %s", id)
		}

		if dir == "" {
			// should be a default dump directory as well?
			if cli.cfg.SharedStorage.DumpStorageDir == "" {
				return fmt.Errorf("no dump directory specified")
			}
			dir = cli.cfg.SharedStorage.DumpStorageDir
			cli.logger.Info().Msgf("no directory specified as input, defaulting to %s", dir)
		}

		// always self serve when invoked from CLI
		dumpArgs := task.DumpArgs{
			PID:   int32(pid),
			Dir:   dir,
			JobID: id,
			Type:  task.DumpArgs_SELF_SERVE,
		}

		resp := cli.cts.CheckpointTask(&dumpArgs)

		if resp.Error != "" {
			return fmt.Errorf(resp.Error)
		}

		cli.cts.Close()

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

		dumpArgs := task.ContainerDumpArgs{
			ContainerId: containerId,
			Ref:         ref,
		}
		resp := cli.cts.CheckpointContainer(&dumpArgs)

		if resp.Error != "" {
			return fmt.Errorf(resp.Error)
		}

		cli.cts.Close()

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

		root = "/var/run/runc"

		criuOpts := &task.CriuOpts{
			ImagesDirectory: runcPath,
			WorkDirectory:   workPath,
			LeaveRunning:    true,
			TcpEstablished:  false,
		}

		dumpArgs := task.RuncDumpArgs{
			Root:           root,
			CheckpointPath: checkpointPath,
			ContainerId:    containerId,
			CriuOpts:       criuOpts,
		}

		resp := cli.cts.CheckpointRunc(&dumpArgs)

		if resp.Error != "" {
			return fmt.Errorf(resp.Error)
		}

		cli.cts.Close()

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

		opts := &task.RuncOpts{
			Root:          root,
			Bundle:        bundle,
			ConsoleSocket: consoleSocket,
			Detatch:       detach,
		}

		restoreArgs := &task.RuncRestoreArgs{
			ImagePath:   runcPath,
			ContainerId: containerId,
			Opts:        opts,
		}

		resp := cli.cts.RuncRestore(restoreArgs)

		if resp.Error != "" {
			return fmt.Errorf(resp.Error)
		}

		cli.cts.Close()

		cli.logger.Info().Msgf("container %s successfully restored", containerId)

		return nil
	},
}

var restoreCmd = &cobra.Command{
	Use:   "restore",
	Short: "Manually restore a process or container from a checkpoint located at input path: [process, runc (container), containerd (container)]",
}

var restoreProcessCmd = &cobra.Command{
	Use:   "process",
	Short: "Manually restore a process from a checkpoint located at input path",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cli, err := NewCLI()
		if err != nil {
			return err
		}

		restoreArgs := task.RestoreArgs{
			CheckpointId: "Not Implemented",
			Dir:          args[0],
		}

		resp := cli.cts.RestoreTask(&restoreArgs)

		if resp.Error != "" {
			return fmt.Errorf(resp.Error)
		}

		cli.cts.Close()

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

		restoreArgs := &task.ContainerRestoreArgs{
			ImgPath:     imgPath,
			ContainerId: containerId,
		}

		resp := cli.cts.ContainerRestore(restoreArgs)

		if resp.Error != "" {
			return fmt.Errorf(resp.Error)
		}

		cli.cts.Close()

		cli.logger.Info().Msgf("container %s restored from %s successfully", containerId, ref)
		return nil
	},
}

var execTaskCmd = &cobra.Command{
	Use:   "exec",
	Short: "Start and register a new process with Cedana",
	Long:  "Start and register a process by passing a task + id pair (cedana start <task> <id>)",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		cli, err := NewCLI()
		if err != nil {
			return err
		}

		taskArgs := &task.StartTaskArgs{
			Task: args[0],
			Id:   args[1],
		}

		resp := cli.cts.StartTask(taskArgs)

		if resp.Error != "" {
			return fmt.Errorf(resp.Error)
		}

		cli.cts.Close()
		return nil
	},
}

var psCmd = &cobra.Command{
	Use:   "ps",
	Short: "List running processes",
	RunE: func(cmd *cobra.Command, args []string) error {
		cli, err := NewCLI()
		if err != nil {
			return err
		}

		// open db in read-only mode
		conn, err := bolt.Open("/tmp/cedana.db", 0600, &bolt.Options{ReadOnly: true})
		if err != nil {
			cli.logger.Fatal().Err(err).Msg("Could not open or create db")
			return err
		}

		defer conn.Close()
		var idPid []map[string]string
		var pidState []map[string]string
		err = conn.View(func(tx *bolt.Tx) error {
			root := tx.Bucket([]byte("default"))
			if root == nil {
				return fmt.Errorf("could not find bucket")
			}

			root.ForEachBucket(func(k []byte) error {
				job := root.Bucket(k)
				jobId := string(k)
				job.ForEach(func(k, v []byte) error {
					idPid = append(idPid, map[string]string{
						jobId: string(v),
					})
					pidState = append(pidState, map[string]string{
						string(k): string(v),
					})

					return nil
				})
				return nil
			})

			if err != nil {
				return err
			}
			return nil
		})

		for _, v := range idPid {
			fmt.Printf("%s\n", v)
		}

		for _, v := range pidState {
			fmt.Printf("%s\n", v)
		}

		return err
	},
}

func initRuncCommands() {
	runcRestoreCmd.Flags().StringVarP(&runcPath, "image", "i", "", "image path")
	runcRestoreCmd.MarkFlagRequired("image")
	runcRestoreCmd.Flags().StringVarP(&containerId, "id", "p", "", "container id")
	runcRestoreCmd.MarkFlagRequired("id")
	runcRestoreCmd.Flags().StringVarP(&bundle, "bundle", "b", "", "bundle path")
	runcRestoreCmd.MarkFlagRequired("bundle")
	runcRestoreCmd.Flags().StringVarP(&consoleSocket, "console-socket", "c", "", "console socket path")
	runcRestoreCmd.Flags().StringVarP(&root, "root", "r", "/var/run/runc", "runc root directory")
	runcRestoreCmd.Flags().BoolVarP(&detach, "detach", "d", false, "run runc container in detached mode")

	restoreCmd.AddCommand(runcRestoreCmd)

	runcDumpCmd.Flags().StringVarP(&runcPath, "image", "i", "", "image path")
	runcDumpCmd.MarkFlagRequired("image")
	runcDumpCmd.Flags().StringVarP(&containerId, "id", "p", "", "container id")
	runcDumpCmd.MarkFlagRequired("id")

	dumpCmd.AddCommand(runcDumpCmd)
}

func initContainerdCommands() {
	containerdDumpCmd.Flags().StringVarP(&ref, "image", "i", "", "image checkpoint path")
	containerdDumpCmd.MarkFlagRequired("image")
	containerdDumpCmd.Flags().StringVarP(&containerId, "id", "p", "", "container id")
	containerdDumpCmd.MarkFlagRequired("id")

	dumpCmd.AddCommand(containerdDumpCmd)

	containerdRestoreCmd.Flags().StringVarP(&ref, "image", "i", "", "image ref")
	containerdRestoreCmd.MarkFlagRequired("image")
	containerdRestoreCmd.Flags().StringVarP(&containerId, "id", "p", "", "container id")
	containerdRestoreCmd.MarkFlagRequired("id")

	restoreCmd.AddCommand(containerdRestoreCmd)
}

func init() {
	dumpCmd.AddCommand(dumpProcessCmd)
	dumpProcessCmd.Flags().StringVarP(&dir, "dir", "d", "", "directory to dump to")
	dumpProcessCmd.Flags().StringVarP(&id, "jobid", "j", "", "optionally specify an id (randomly generated if omitted)")
	dumpProcessCmd.MarkFlagRequired("dir")

	restoreCmd.AddCommand(restoreProcessCmd)

	rootCmd.AddCommand(dumpCmd)
	rootCmd.AddCommand(restoreCmd)
	rootCmd.AddCommand(execTaskCmd)
	rootCmd.AddCommand(psCmd)

	initRuncCommands()

	initContainerdCommands()

}

package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"

	"github.com/cedana/cedana/api"
	"github.com/cedana/cedana/api/services"
	"github.com/cedana/cedana/api/services/task"
	"github.com/cedana/cedana/utils"
	"github.com/olekukonko/tablewriter"
	"github.com/rs/xid"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
	"google.golang.org/grpc/status"

	bolt "go.etcd.io/bbolt"
)

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

// working directory for execTask
var wd string
var execAsRoot bool

type CLI struct {
	cfg    *utils.Config
	cts    *services.ServiceClient
	logger zerolog.Logger
}

func NewCLI() (*CLI, error) {
	ctx := context.Background()

	cfg, err := utils.InitConfig()
	if err != nil {
		return nil, err
	}
	cts := services.NewClient("localhost:8080", ctx)

	logger := utils.GetLogger()

	return &CLI{
		cfg:    cfg,
		cts:    cts,
		logger: logger,
	}, nil
}

// --------------------
// Top-level Dump/Restore CLI commands
// --------------------

var dumpCmd = &cobra.Command{
	Use:   "dump",
	Short: "Manually checkpoint a process or container to a directory: [process, runc (container), containerd (container)]",
}

// ---------------
// Manual checkpoint/restore with a PID
// ---------------

var dumpProcessCmd = &cobra.Command{
	Use:   "process",
	Short: "Manually checkpoint a running process [pid] to a directory [-d]",
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

		id := xid.New().String()
		cli.logger.Info().Msgf("no id specified, defaulting to %s", id)

		if dir == "" {
			// TODO NR - should we default to /tmp?
			if cli.cfg.SharedStorage.DumpStorageDir == "" {
				return fmt.Errorf("no dump directory specified")
			}
			dir = cli.cfg.SharedStorage.DumpStorageDir
			cli.logger.Info().Msgf("no directory specified as input, defaulting to %s", dir)
		}

		// always self serve when invoked from CLI
		cpuDumpArgs := task.DumpArgs{
			PID:   int32(pid),
			Dir:   dir,
			JobID: id,
			Type:  task.DumpArgs_LOCAL,
		}

		resp, err := cli.cts.CheckpointTask(&cpuDumpArgs)
		if err != nil {
			st, ok := status.FromError(err)
			if ok {
				cli.logger.Error().Msgf("Checkpoint task failed: %v, %v: %v", st.Code(), st.Message(), st.Details())
			} else {
				cli.logger.Error().Msgf("Checkpoint task failed: %v", err)
			}
		} else {
			cli.logger.Info().Msgf("Response: %v", resp.Message)
		}

		cli.cts.Close()

		return nil
	},
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
			CheckpointId:   "Not Implemented",
			CheckpointPath: args[0],
		}

		resp, err := cli.cts.RestoreTask(&restoreArgs)
		if err != nil {
			st, ok := status.FromError(err)
			if ok {
				cli.logger.Error().Msgf("Restore task failed: %v, %v: %v", st.Code(), st.Message(), st.Details())
			} else {
				cli.logger.Error().Msgf("Restore task failed: %v", err)
			}
		}

		cli.logger.Info().Msgf("Response: %v", resp.Message)

		cli.cts.Close()

		return nil
	},
}

// -----------------
// Checkpoint/restore of a job w/ ID (currently limited to processes)
// -----------------

var dumpJobCmd = &cobra.Command{
	Use:   "job",
	Args:  cobra.ExactArgs(1),
	Short: "Manually checkpoint a running job to a directory",
	RunE: func(cmd *cobra.Command, args []string) error {
		// TODO NR - this needs to be extended to include container checkpoints
		cli, err := NewCLI()
		if err != nil {
			return err
		}

		if args[0] == "" {
			return fmt.Errorf("no job id specified")
		}

		id := args[0]

		if dir == "" {
			if cli.cfg.SharedStorage.DumpStorageDir == "" {
				return fmt.Errorf("no dump directory specified")
			}
			dir = cli.cfg.SharedStorage.DumpStorageDir
			cli.logger.Info().Msgf("no directory specified as input, defaulting to %s", dir)
		}

		// get PID of running job
		// TODO NR - we should be querying the API for this instead of
		// directly opening the db. Permissions issue
		db := api.NewDB()
		pid, err := db.GetPID(id)
		if err != nil {
			return err
		}

		if pid == 0 {
			return fmt.Errorf("pid 0 returned from state - is process running?")
		}

		var taskType task.DumpArgs_DumpType
		if os.Getenv("CEDANA_REMOTE") == "true" {
			taskType = task.DumpArgs_REMOTE
		} else {
			taskType = task.DumpArgs_LOCAL
		}

		dumpArgs := task.DumpArgs{
			PID:   pid,
			JobID: id,
			Dir:   dir,
			Type:  taskType,
		}

		resp, err := cli.cts.CheckpointTask(&dumpArgs)
		if err != nil {
			st, ok := status.FromError(err)
			if ok {
				cli.logger.Error().Msgf("Checkpoint task failed: %v: %v", st.Code(), st.Message())
			} else {
				cli.logger.Error().Msgf("Checkpoint task failed: %v", err)
			}
		}

		cli.logger.Info().Msgf("Response: %v", resp.Message)

		cli.cts.Close()

		return err
	},
}

var restoreJobCmd = &cobra.Command{
	Use:   "job",
	Short: "Manually restore a process or container from an input id",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// TODO NR - add support for containers, currently supports only process
		cli, err := NewCLI()
		if err != nil {
			return err
		}

		db := api.NewDB()

		var restoreArgs task.RestoreArgs
		if os.Getenv("CEDANA_REMOTE") == "true" {
			jobState, err := db.GetStateFromID(args[0])
			if err != nil {
				return err
			}

			remoteState := jobState.GetRemoteState()
			if remoteState == nil {
				return fmt.Errorf("no remote state found for id %s", args[0])
			}

			if remoteState.CheckpointID == "" {
				return fmt.Errorf("no checkpoint found for id %s", args[0])
			}

			restoreArgs = task.RestoreArgs{
				CheckpointId:   remoteState.CheckpointID,
				CheckpointPath: "",
				Type:           task.RestoreArgs_REMOTE,
			}
		} else {
			paths, err := db.GetLatestLocalCheckpoints(args[0])
			if err != nil {
				return err
			}

			if len(paths) == 0 {
				return fmt.Errorf("no checkpoint found for id %s", args[0])
			}

			fmt.Printf("paths: %v\n", paths)

			checkpointPath := *paths[0]
			restoreArgs = task.RestoreArgs{
				CheckpointId:   "",
				CheckpointPath: checkpointPath,
				Type:           task.RestoreArgs_LOCAL,
			}
		}
		// pass path to restore task
		resp, err := cli.cts.RestoreTask(&restoreArgs)
		if err != nil {
			st, ok := status.FromError(err)
			if ok {
				cli.logger.Error().Msgf("Restore task failed: %v: %v", st.Code(), st.Message())
			} else {
				cli.logger.Error().Msgf("Restore task failed: %v", err)
			}
		}

		cli.logger.Info().Msgf("Response: %v", resp.Message)

		cli.cts.Close()

		return nil
	},
}

// -----------------------
// Checkpoint/Restore of a containerd container
// -----------------------

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
		resp, err := cli.cts.CheckpointContainer(&dumpArgs)
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

		resp, err := cli.cts.RestoreContainer(restoreArgs)
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
		}

		restoreArgs := &task.RuncRestoreArgs{
			ImagePath:   runcPath,
			ContainerId: containerId,
			Opts:        opts,
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

var restoreCmd = &cobra.Command{
	Use:   "restore",
	Short: "Manually restore a process or container from a checkpoint located at input path: [process, runc (container), containerd (container)]",
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

		var uid uint32
		var gid uint32

		if !execAsRoot {
			uid = uint32(os.Getuid())
			gid = uint32(os.Getgid())
		}

		taskArgs := &task.StartTaskArgs{
			Task:       args[0],
			Id:         args[1],
			WorkingDir: wd,
			UID:        uid,
			GID:        gid,
		}

		resp, err := cli.cts.StartTask(taskArgs)
		if err != nil {
			st, ok := status.FromError(err)
			if ok {
				cli.logger.Error().Msgf("Start task failed: %v, %v", st.Message(), st.Code())
			} else {
				cli.logger.Error().Msgf("Start task failed: %v", err)
			}
		}

		cli.logger.Info().Msgf("Response: %v", resp.Message)

		cli.cts.Close()
		fmt.Print(resp.PID)
		return nil
	},
}

var psCmd = &cobra.Command{
	Use:   "ps",
	Short: "List managed processes",
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

		// job ID, PID, isRunning, CheckpointPath, Remote checkpoint ID
		var data [][]string
		err = conn.View(func(tx *bolt.Tx) error {
			root := tx.Bucket([]byte("default"))
			if root == nil {
				return fmt.Errorf("could not find bucket")
			}

			return root.ForEachBucket(func(k []byte) error {
				job := root.Bucket(k)
				jobId := string(k)
				return job.ForEach(func(k, v []byte) error {
					var state task.ProcessState
					var remoteCheckpointID string
					var status string
					err := json.Unmarshal(v, &state)
					if err != nil {
						return err
					}

					if state.RemoteState != nil {
						remoteCheckpointID = state.RemoteState.CheckpointID
					}

					if state.ProcessInfo != nil {
						status = state.ProcessInfo.Status
					}

					data = append(data, []string{jobId, string(k), status, state.CheckpointPath, remoteCheckpointID})
					return nil
				})
			})
		})

		if err != nil {
			return err
		}

		table := tablewriter.NewWriter(os.Stdout)
		table.SetHeader([]string{"Job ID", "PID", "Status", "Local Checkpoint Path", "Remote Checkpoint ID"})

		for _, v := range data {
			table.Append(v)
		}

		table.Render() // Send output
		return nil
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
	dumpProcessCmd.MarkFlagRequired("dir")

	dumpCmd.AddCommand(dumpJobCmd)
	dumpJobCmd.Flags().StringVarP(&dir, "dir", "d", "", "directory to dump to")

	restoreCmd.AddCommand(restoreProcessCmd)
	restoreCmd.AddCommand(restoreJobCmd)

	execTaskCmd.Flags().StringVarP(&wd, "working-dir", "w", "", "working directory")
	execTaskCmd.Flags().BoolVarP(&execAsRoot, "root", "r", false, "run as root")

	rootCmd.AddCommand(dumpCmd)
	rootCmd.AddCommand(restoreCmd)
	rootCmd.AddCommand(execTaskCmd)
	rootCmd.AddCommand(psCmd)

	initRuncCommands()

	initContainerdCommands()
}

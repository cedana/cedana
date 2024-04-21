package cmd

// This file contains all the client commands for interacting with the daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"

	"github.com/cedana/cedana/api"
	"github.com/cedana/cedana/api/services"
	"github.com/cedana/cedana/api/services/task"
	"github.com/olekukonko/tablewriter"
	"github.com/rs/xid"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"google.golang.org/grpc/status"

	bolt "go.etcd.io/bbolt"
)

var (
	dir string
	ref string
)

var (
	containerId    string
	imgPath        string
	runcPath       string
	root           string
	checkpointPath string
	workPath       string
)

var (
	bundle        string
	consoleSocket string
	detach        bool
	netPid        int32
)

var (
	isK3s          bool
	tcpEstablished bool
)

// working directory for execTask
var (
	wd     string
	asRoot bool
)

// --------------------
// Top-level Dump/Restore CLI commands
// --------------------

var dumpCmd = &cobra.Command{
	Use:   "dump",
	Short: "Manually checkpoint a process or container to a directory: [process, runc (container), containerd (container)]",
}

var restoreCmd = &cobra.Command{
	Use:   "restore",
	Short: "Manually restore a process or container from a checkpoint located at input path: [process, runc (container), containerd (container)]",
}

// ---------------
// Manual checkpoint/restore with a PID
// ---------------

var dumpProcessCmd = &cobra.Command{
	Use:   "process",
	Short: "Manually checkpoint a running process [pid] to a directory [-d]",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		cts, err := services.NewClient(api.Address)
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
		logger.Info().Msgf("no id specified, defaulting to %s", id)

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
			PID:   int32(pid),
			Dir:   dir,
			JobID: id,
			Type:  task.DumpArgs_LOCAL,
		}

		resp, err := cts.CheckpointTask(&cpuDumpArgs)
		if err != nil {
			st, ok := status.FromError(err)
			if ok {
				logger.Error().Msgf("Checkpoint task failed: %v, %v: %v", st.Code(), st.Message(), st.Details())
			} else {
				logger.Error().Msgf("Checkpoint task failed: %v", err)
			}
		} else {
			logger.Info().Msgf("Response: %v", resp.Message)
		}
	},
}

var restoreProcessCmd = &cobra.Command{
	Use:   "process",
	Short: "Manually restore a process from a checkpoint located at input path",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		cts, err := services.NewClient(api.Address)
		if err != nil {
			logger.Error().Msgf("Error creating client: %v", err)
			return
		}
		defer cts.Close()

		restoreArgs := task.RestoreArgs{
			CheckpointId:   "Not Implemented",
			CheckpointPath: args[0],
		}

		resp, err := cts.RestoreTask(&restoreArgs)
		if err != nil {
			st, ok := status.FromError(err)
			if ok {
				logger.Error().Msgf("Restore task failed: %v, %v: %v", st.Code(), st.Message(), st.Details())
			} else {
				logger.Error().Msgf("Restore task failed: %v", err)
			}
		}

		logger.Info().Msgf("Response: %v", resp.Message)
	},
}

// -----------------
// Checkpoint/restore of a job w/ ID (currently limited to processes)
// -----------------

var dumpJobCmd = &cobra.Command{
	Use: "job",
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) != 1 {
			return fmt.Errorf("requires a job id argument, use cedana ps to see available jobs")
		}
		return nil
	},
	Short: "Manually checkpoint a running job to a directory",
	Run: func(cmd *cobra.Command, args []string) {
		// TODO NR - this needs to be extended to include container checkpoints
		cts, err := services.NewClient(api.Address)
		if err != nil {
			logger.Error().Msgf("Error creating client: %v", err)
			return
		}
		defer cts.Close()

		if args[0] == "" {
			logger.Error().Msgf("no job id specified")
		}

		id := args[0]

		if dir == "" {
			dir = viper.GetString("shared_storage.dump_storage_dir")
			if dir == "" {
				logger.Error().Msgf("no dump directory specified")
				return
			}
			logger.Info().Msgf("no directory specified as input, using %s from config", dir)
		}

		var taskType task.DumpArgs_DumpType
		if viper.GetBool("remote") {
			taskType = task.DumpArgs_REMOTE
		} else {
			taskType = task.DumpArgs_LOCAL
		}

		dumpArgs := task.DumpArgs{
			JobID: id,
			Dir:   dir,
			Type:  taskType,
		}

		resp, err := cts.CheckpointTask(&dumpArgs)
		if err != nil {
			st, ok := status.FromError(err)
			if ok {
				logger.Error().Msgf("Checkpoint task failed: %v: %v", st.Code(), st.Message())
			} else {
				logger.Error().Msgf("Checkpoint task failed: %v", err)
			}
		}

		logger.Info().Msgf("Response: %v", resp.Message)
	},
}

var restoreJobCmd = &cobra.Command{
	Use:   "job",
	Short: "Manually restore a previously dumped process or container from an input id",
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) != 1 {
			return fmt.Errorf("requires a job id argument, use cedana ps to see available jobs")
		}
		return nil
	},
	Run: func(cmd *cobra.Command, args []string) {
		cts, err := services.NewClient(api.Address)
		if err != nil {
			logger.Error().Msgf("Error creating client: %v", err)
			return
		}
		defer cts.Close()

		// TODO NR: we shouldn't even be reading the db here!!
		db := api.NewDB()

		var uid uint32
		var gid uint32

		if !asRoot {
			uid = uint32(os.Getuid())
			gid = uint32(os.Getgid())
		}

		var restoreArgs task.RestoreArgs
		if viper.GetBool("remote") {
			jobState, err := db.GetStateFromID(args[0])
			if err != nil {
				logger.Error().Msgf("Error getting state from id: %v", err)
				return
			}

			remoteState := jobState.GetRemoteState()
			if remoteState == nil {
				logger.Error().Msgf("No remote state found for id %s", args[0])
				return
			}

			// For now just grab latest checkpoint
			if remoteState[len(remoteState)-1].CheckpointID == "" {
				logger.Error().Msgf("No checkpoint found for id %s", args[0])
				return
			}

			restoreArgs = task.RestoreArgs{
				CheckpointId:   remoteState[len(remoteState)-1].CheckpointID,
				CheckpointPath: "",
				Type:           task.RestoreArgs_REMOTE,
				JobID:          args[0],
				UID:            uid,
				GID:            gid,
			}
		} else {
			paths, err := db.GetLatestLocalCheckpoints(args[0])
			if err != nil {
				logger.Error().Msgf("Error getting latest local checkpoints: %v", err)
				return
			}

			if len(paths) == 0 {
				logger.Error().Msgf("no checkpoint found for id %s", args[0])
				return
			}

			fmt.Printf("paths: %v\n", paths)

			checkpointPath := *paths[0]
			restoreArgs = task.RestoreArgs{
				CheckpointId:   "",
				CheckpointPath: checkpointPath,
				Type:           task.RestoreArgs_LOCAL,
				JobID:          args[0],
				UID:            uid,
				GID:            gid,
			}
		}
		// pass path to restore task
		resp, err := cts.RestoreTask(&restoreArgs)
		if err != nil {
			st, ok := status.FromError(err)
			if ok {
				logger.Error().Msgf("Restore task failed: %v: %v", st.Code(), st.Message())
			} else {
				logger.Error().Msgf("Restore task failed: %v", err)
			}
		}

		logger.Info().Msgf("Response: %v", resp.Message)
	},
}

// -----------------------
// Checkpoint/Restore of a containerd container
// -----------------------

var containerdDumpCmd = &cobra.Command{
	Use:   "containerd",
	Short: "Manually checkpoint a running container to a directory",
	Args:  cobra.ArbitraryArgs,
	Run: func(cmd *cobra.Command, args []string) {
		cts, err := services.NewClient(api.Address)
		if err != nil {
			logger.Error().Msgf("Error creating client: %v", err)
			return
		}
		defer cts.Close()

		dumpArgs := task.ContainerDumpArgs{
			ContainerId: containerId,
			Ref:         ref,
		}
		resp, err := cts.CheckpointContainer(&dumpArgs)
		if err != nil {
			st, ok := status.FromError(err)
			if ok {
				logger.Error().Msgf("Checkpoint task failed: %v, %v", st.Message(), st.Code())
			} else {
				logger.Error().Msgf("Checkpoint task failed: %v", err)
			}
		}

		logger.Info().Msgf("Response: %v", resp.Message)
	},
}

var containerdRestoreCmd = &cobra.Command{
	Use:   "containerd",
	Short: "Manually checkpoint a running container to a directory",
	Args:  cobra.ArbitraryArgs,
	Run: func(cmd *cobra.Command, args []string) {
		cts, err := services.NewClient(api.Address)
		if err != nil {
			logger.Error().Msgf("Error creating client: %v", err)
			return
		}
		defer cts.Close()

		restoreArgs := &task.ContainerRestoreArgs{
			ImgPath:     ref,
			ContainerId: containerId,
		}

		resp, err := cts.RestoreContainer(restoreArgs)
		if err != nil {
			st, ok := status.FromError(err)
			if ok {
				logger.Error().Msgf("Restore task failed: %v, %v", st.Message(), st.Code())
			} else {
				logger.Error().Msgf("Restore task failed: %v", err)
			}
		}

		logger.Info().Msgf("Response: %v", resp.Message)
	},
}

var execTaskCmd = &cobra.Command{
	Use:   "exec",
	Short: "Start and register a new process with Cedana",
	Long:  "Start and register a process by passing a task + id pair (cedana start <task> <id>)",
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) != 2 {
			return fmt.Errorf("requires a task and id argument")
		}
		return nil
	},
	Run: func(cmd *cobra.Command, args []string) {
		cts, err := services.NewClient(api.Address)
		if err != nil {
			logger.Error().Msgf("Error creating client: %v", err)
			return
		}
		defer cts.Close()

		var env []string
		var uid uint32
		var gid uint32
		var taskToExec string = args[0]

		if !asRoot {
			uid = uint32(os.Getuid())
			gid = uint32(os.Getgid())
		}

		// should this be gated w/ a flag?
		env = os.Environ()

		taskArgs := &task.StartTaskArgs{
			Task:       taskToExec,
			Id:         args[1],
			WorkingDir: wd,
			Env:        env,
			UID:        uid,
			GID:        gid,
		}

		resp, err := cts.StartTask(taskArgs)
		if err != nil {
			st, ok := status.FromError(err)
			if ok {
				logger.Error().Msgf("Start task failed: %v, %v", st.Message(), st.Code())
			} else {
				logger.Error().Msgf("Start task failed: %v", err)
			}
		}

		fmt.Print(resp.PID)
	},
}

var psCmd = &cobra.Command{
	Use:   "ps",
	Short: "List managed processes",
	Run: func(cmd *cobra.Command, args []string) {
		cts, err := services.NewClient(api.Address)
		if err != nil {
			logger.Error().Msgf("Error creating client: %v", err)
			return
		}
		defer cts.Close()

		// open db in read-only mode
		conn, err := bolt.Open("/tmp/cedana.db", 0600, &bolt.Options{ReadOnly: true})
		if err != nil {
			logger.Fatal().Err(err).Msg("Could not open or create db")
			return
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
						// For now just grab latest checkpoint
						remoteCheckpointID = state.RemoteState[len(state.RemoteState)-1].CheckpointID
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
			logger.Error().Msgf("Error getting job data: %v", err)
			return
		}

		table := tablewriter.NewWriter(os.Stdout)
		table.SetHeader([]string{"Job ID", "PID", "Status", "Local Checkpoint Path", "Remote Checkpoint ID"})

		for _, v := range data {
			table.Append(v)
		}

		table.Render() // Send output
	},
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
	restoreJobCmd.Flags().BoolVarP(&asRoot, "root", "r", false, "restore as root")

	execTaskCmd.Flags().StringVarP(&wd, "working-dir", "w", "", "working directory")
	execTaskCmd.Flags().BoolVarP(&asRoot, "root", "r", false, "run as root")

	rootCmd.AddCommand(dumpCmd)
	rootCmd.AddCommand(restoreCmd)
	rootCmd.AddCommand(execTaskCmd)
	rootCmd.AddCommand(psCmd)
	rootCmd.AddCommand(runcRoot)

	initRuncCommands()
	initContainerdCommands()
}

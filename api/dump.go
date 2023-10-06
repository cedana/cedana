package api

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/cedana/cedana/api/services/task"
	container "github.com/cedana/cedana/container"
	"github.com/cedana/cedana/types"
	"github.com/cedana/cedana/utils"
	"github.com/checkpoint-restore/go-criu/v6/rpc"
	"google.golang.org/protobuf/proto"
)

const (
	sys_pidfd_send_signal = 424
	sys_pidfd_open        = 434
	sys_pidfd_getfd       = 438
)

// Signals a process prior to dumping with SIGUSR1 and outputs any created checkpoints
func (c *Client) signalProcessAndWait(pid int32, timeout int) *string {
	var checkpointPath string
	fd, _, errno := syscall.Syscall(sys_pidfd_open, uintptr(pid), 0, 0)
	if errno != 0 {
		c.logger.Fatal().Err(errno).Msg("could not open pid")
	}
	s := syscall.SIGUSR1
	_, _, errno = syscall.Syscall6(sys_pidfd_send_signal, uintptr(fd), uintptr(s), 0, 0, 0, 0)
	if errno != 0 {
		c.logger.Info().Msgf("could not send signal to pid %d", pid)
	}

	// we want to sleep the dumping thread here to wait for the process
	// to finish executing. This likely won't have any effects when run in daemon mode,
	// it'll just pause the spawned dump goroutine

	// while we wait, try and get the fd of the checkpoint as its being written
	state, err := c.getState(pid)
	if err != nil {
		c.logger.Fatal().Err(err).Msg("could not get state")
	}
	for _, f := range state.ProcessInfo.OpenFds {
		// TODO NR: add more checkpoint options
		if strings.Contains(f.Path, "pt") {
			sfi, err := os.Stat(f.Path)
			if err != nil {
				continue
			}
			if sfi.Mode().IsRegular() {
				checkpointPath = f.Path
			}
		}
	}

	time.Sleep(time.Duration(timeout) * time.Second)

	return &checkpointPath
}

func (c *Client) prepareDump(pid int32, dir string, opts *rpc.CriuOpts) (string, error) {
	pname, err := utils.GetProcessName(pid)
	if err != nil {
		c.logger.Fatal().Err(err)
		return "", err
	}

	state, err := c.getState(pid)
	if state == nil || err != nil {
		return "", fmt.Errorf("could not get state")
	}

	// check network connections
	var hasTCP bool
	var hasExtUnixSocket bool

	for _, conn := range state.ProcessInfo.OpenConnections {
		if conn.Type == syscall.SOCK_STREAM { // TCP
			hasTCP = true
		}

		if conn.Type == syscall.AF_UNIX { // Interprocess
			hasExtUnixSocket = true
		}
	}
	opts.TcpEstablished = proto.Bool(hasTCP)
	opts.ExtUnixSk = proto.Bool(hasExtUnixSocket)

	opts.FileLocks = proto.Bool(true)

	// check tty state
	// if pts is in open fds, chances are it's a shell job
	var isShellJob bool
	for _, f := range state.ProcessInfo.OpenFds {
		if strings.Contains(f.Path, "pts") {
			isShellJob = true
			break
		}
	}
	opts.ShellJob = proto.Bool(isShellJob)

	// processname + datetime
	// strip out non posix-compliant characters from the processname
	formattedProcessName := regexp.MustCompile("[^a-zA-Z0-9_.-]").ReplaceAllString(*pname, "_")
	formattedProcessName = strings.ReplaceAll(formattedProcessName, ".", "_")
	processCheckpointDir := strings.Join([]string{formattedProcessName, time.Now().Format("02_01_2006_1504")}, "_")
	checkpointFolderPath := filepath.Join(dir, processCheckpointDir)
	_, err = os.Stat(filepath.Join(checkpointFolderPath))
	if err != nil {
		// if dir in config is ~./cedana, this creates ~./cedana/exampleProcess_2020_01_02_15_04/
		// the folder path is passed to CRIU, which creates memory dumps and other checkpoint images into the folder
		if err := os.Mkdir(checkpointFolderPath, 0o755); err != nil {
			return "", err
		}
	}

	c.copyOpenFiles(checkpointFolderPath, state)
	// c.channels.preDumpBroadcaster.Broadcast(1)

	return checkpointFolderPath, nil
}

// Copies open writeonly files to dumpdir to ensure consistency on restore.
// TODO NR: should we add a check for filesize here? Worried about dealing with massive files.
// This can be potentially fixed with barriers, which also assumes that massive (>10G) files are being
// written to on network storage or something.
func (c *Client) copyOpenFiles(dir string, state *task.ProcessState) error {
	if len(state.ProcessInfo.OpenWriteOnlyFilePaths) == 0 {
		return nil
	}
	for _, f := range state.ProcessInfo.OpenWriteOnlyFilePaths {
		if err := utils.CopyFile(f, dir); err != nil {
			return err
		}
	}

	return nil
}

// we pass a final state to postDump so we can serialize at the exact point
// the checkpoint was written.
func (c *Client) postDump(dumpdir string, state *task.ProcessState) {
	c.logger.Info().Msg("compressing checkpoint...")
	compressedCheckpointPath := strings.Join([]string{dumpdir, ".zip"}, "")

	// copy open writeonly fds one more time
	// TODO NR - this is a wasted operation - should check if bytes have been written
	// post checkpoint
	err := c.copyOpenFiles(dumpdir, state)
	if err != nil {
		c.logger.Fatal().Err(err)
	}

	state.CheckpointPath = compressedCheckpointPath
	// sneak in a serialized state obj
	err = types.SerializeToFolder(dumpdir, state)
	if err != nil {
		c.logger.Fatal().Err(err)
	}

	c.logger.Info().Msgf("compressing checkpoint to %s", compressedCheckpointPath)

	err = utils.ZipFolder(dumpdir, compressedCheckpointPath)
	if err != nil {
		c.logger.Fatal().Err(err)
	}

	if c.config.CedanaManaged {
		c.logger.Info().Msg("client is managed by a cedana orchestrator, pushing checkpoint..")
		// TODO BS: This should just updating cedana market db
		// err := c.store.PushCheckpoint(compressedCheckpointPath)
		if err != nil {
			c.logger.Info().Msgf("error pushing checkpoint: %v", err)
		}
	}

	c.db.UpdateProcessStateWithID(c.jobId, state)
}

func (c *Client) prepareCheckpointOpts() *rpc.CriuOpts {
	opts := rpc.CriuOpts{
		LogLevel:     proto.Int32(4),
		LogFile:      proto.String("dump.log"),
		LeaveRunning: proto.Bool(c.config.Client.LeaveRunning),
		GhostLimit:   proto.Uint32(uint32(10000000)),
		ExtMasters:   proto.Bool(true),
	}
	return &opts

}

func (c *Client) RuncDump(root string, containerId string, opts *container.CriuOpts) error {
	runcContainer := container.GetContainerFromRunc(containerId, root)

	err := runcContainer.RuncCheckpoint(opts, runcContainer.Pid)
	if err != nil {
		c.logger.Fatal().Err(err)
	}
	return nil
}

func (c *Client) ContainerDump(dir string, containerId string) error {
	err := container.Dump(dir, containerId)
	if err != nil {
		c.logger.Fatal().Err(err)
	}
	return nil
}

func (c *Client) Dump(dir string, pid int32) error {
	defer c.timeTrack(time.Now(), "dump")

	jobId, exists := os.LookupEnv("CEDANA_JOB_ID")
	if exists {
		c.jobId = jobId
	}

	opts := c.prepareCheckpointOpts()
	dumpdir, err := c.prepareDump(pid, dir, opts)
	if err != nil {
		return err
	}

	c.CheckpointDir = dumpdir

	img, err := os.Open(dumpdir)
	if err != nil {
		c.logger.Warn().Msgf("could not open checkpoint storage dir %s with error: %v", dir, err)
		return err
	}
	defer img.Close()

	opts.ImagesDirFd = proto.Int32(int32(img.Fd()))
	opts.Pid = proto.Int32(pid)

	nfy := utils.Notify{
		Config:          c.config,
		Logger:          c.logger,
		PreDumpAvail:    true,
		PostDumpAvail:   true,
		PreRestoreAvail: true,
	}

	c.logger.Info().Msgf(`beginning dump of pid %d`, pid)
	state, err := c.generateState(pid)
	if err != nil {
		c.logger.Warn().Msgf("could not generate state: %v", err)
		return err
	}
	if !state.ProcessInfo.AttachedToHardwareAccel {
		_, err = c.CRIU.Dump(opts, &nfy)
		if err != nil {
			// check for sudo error
			if strings.Contains(err.Error(), "errno 0") {
				c.logger.Warn().Msgf("error dumping, cedana is not running as root: %v", err)
				return err
			}

			c.logger.Warn().Msgf("error dumping process: %v", err)
			return err
		}
	}

	// CRIU ntfy hooks get run before this,
	// so have to ensure that image files aren't tampered with
	c.state.CheckpointState = task.CheckpointState_CHECKPOINTED
	c.postDump(dumpdir, state)
	c.cleanupClient()

	return nil
}

package api

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/cedana/cedana/api/services/task"
	"github.com/cedana/cedana/utils"
	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/process"
	"github.com/spf13/afero"

	cedana "github.com/cedana/cedana/types"
)

// wrapper over filesystem, useful for mocking in tests
var AppFs = afero.NewOsFs()

type Client struct {
	CRIU *utils.Criu

	Logger *zerolog.Logger
	config *utils.Config

	channels *CommandChannels
	context  context.Context
	Process  *task.ProcessInfo

	jobId  string
	selfId string

	// a single big state glob
	state *task.ClientStateStreamingArgs

	// for dependency-injection of filesystems (useful for testing)
	fs *afero.Afero

	// checkpoint store
	store utils.Store

	CheckpointDir string
}

type Broadcaster[T any] struct {
	subscribers []chan T
	mu          sync.Mutex
}

func (b *Broadcaster[T]) Subscribe() chan T {
	b.mu.Lock()
	defer b.mu.Unlock()
	ch := make(chan T)
	b.subscribers = append(b.subscribers, ch)
	return ch
}

func (b *Broadcaster[T]) Broadcast(data T) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, ch := range b.subscribers {
		ch <- data
	}
}

type CommandChannels struct {
	dumpCmdBroadcaster    Broadcaster[int]
	restoreCmdBroadcaster Broadcaster[cedana.ServerCommand]
	retryCmdBroadcaster   Broadcaster[cedana.ServerCommand]
	preDumpBroadcaster    Broadcaster[int]
}

type ClientLogs struct {
	Timestamp string `json:"timestamp"`
	Source    string `json:"source"`
	Level     string `json:"level"`
	Msg       string `json:"msg"`
}

func InstantiateClient() (*Client, error) {
	// instantiate logger
	logger := utils.GetLogger()

	criu := utils.MakeCriu()
	_, err := criu.GetCriuVersion()
	// TODO BS may err out if criu binaries aren't installed
	if err != nil {
		logger.Fatal().Err(err).Msg("Error checking CRIU version")
		return nil, err
	}
	// prepare client
	err = criu.Prepare()
	if err != nil {
		logger.Fatal().Err(err).Msg("Error preparing CRIU client")
		return nil, err
	}

	config, err := utils.InitConfig()
	if err != nil {
		logger.Fatal().Err(err).Msg("Could not read config")
		return nil, err
	}

	// set up channels for daemon to listen on
	channels := &CommandChannels{
		dumpCmdBroadcaster:    Broadcaster[int]{},
		restoreCmdBroadcaster: Broadcaster[cedana.ServerCommand]{},
		retryCmdBroadcaster:   Broadcaster[cedana.ServerCommand]{},
		preDumpBroadcaster:    Broadcaster[int]{},
	}

	// set up filesystem wrapper
	fs := &afero.Afero{Fs: AppFs}

	return &Client{
		CRIU:     criu,
		Logger:   &logger,
		config:   config,
		channels: channels,
		context:  context.Background(),
		fs:       fs,
	}, nil
}

func (c *Client) cleanupClient() error {
	c.CRIU.Cleanup()
	c.Logger.Info().Msg("cleaning up client")
	return nil
}

// Function called whenever we enter a failed state and need
// to wait for a command from the orchestrator to continue/unstuck the system.
// Ideally we can use this across the board whenever a case props up that requires
// orchestrator/external intervention.
// Takes a flag as input, which is used to craft a state to pass to NATS and waits
// for a signal to exit. Since go blocks until a signal is received, we use a channel.
func (c *Client) enterDoomLoop() *cedana.ServerCommand {
	retryChn := c.channels.retryCmdBroadcaster.Subscribe()
	// c.publishStateOnce(&c.state)
	for {
		select {
		case cmd := <-retryChn:
			c.Logger.Info().Msgf("received recover command")
			return &cmd
		}
	}
}

// sets up subscribers for dump and restore commands
func (c *Client) timeTrack(start time.Time, name string) {
	elapsed := time.Since(start)
	c.Logger.Debug().Msgf("%s took %s", name, elapsed)
}

func (c *Client) getState(pid int32) *task.ClientStateStreamingArgs {

	if pid == 0 {
		return nil
	}

	p, err := process.NewProcess(pid)
	if err != nil {
		// c.Logger.Info().Msgf("Could not instantiate new gopsutil process with error %v", err)
	}

	var openFiles []*task.OpenFilesStat
	var writeOnlyFiles []string
	var openConnections []*task.ConnectionStat
	var flag task.FlagEnum

	if p != nil {
		openFilesOrig, err := p.OpenFiles()
		for _, f := range openFilesOrig {
			openFiles = append(openFiles, &task.OpenFilesStat{
				Fd:   f.Fd,
				Path: f.Path,
			})
		}

		if err != nil {
			// don't want to error out and break
			return nil
		}
		// used for network barriers (TODO: NR)
		openConnectionsOrig, err := p.Connections()
		if err != nil {
			return nil
		}
		for _, c := range openConnectionsOrig {
			Laddr := &task.Addr{
				IP:   c.Laddr.IP,
				Port: c.Laddr.Port,
			}
			Raddr := &task.Addr{
				IP:   c.Raddr.IP,
				Port: c.Raddr.Port,
			}
			openConnections = append(openConnections, &task.ConnectionStat{
				Fd:     c.Fd,
				Family: c.Family,
				Type:   c.Type,
				Laddr:  Laddr,
				Raddr:  Raddr,
				Status: c.Status,
				Pid:    c.Pid,
				Uids:   c.Uids,
			})
		}

		writeOnlyFiles = c.WriteOnlyFds(openFiles, pid)
	}

	memoryUsed, _ := p.MemoryPercent()
	isRunning, _ := p.IsRunning()

	// if the process is actually running, we don't care that
	// we're potentially overriding a failed flag here.
	// In the case of a restored/resuscitated process this is a good thing
	if isRunning {
		flag = task.FlagEnum_JOB_RUNNING
	}

	// this is the status as returned by gopsutil.
	// ideally we want more than this, or some parsing to happen from this end
	status, _ := p.Status()

	m, _ := mem.VirtualMemory()
	h, _ := host.Info()

	// ignore sending network for now, little complicated
	return &task.ClientStateStreamingArgs{
		ProcessInfo: &task.ProcessInfo{
			PID:                    pid,
			OpenFds:                openFiles,
			OpenWriteOnlyFilePaths: writeOnlyFiles,
			MemoryPercent:          memoryUsed,
			IsRunning:              isRunning,
			OpenConnections:        openConnections,
			Status:                 strings.Join(status, ""),
		},
		ClientInfo: &task.ClientInfo{
			Id:              "NOT IMPLEMENTED",
			Hostname:        h.Hostname,
			Platform:        h.Platform,
			OS:              h.OS,
			Uptime:          h.Uptime,
			RemainingMemory: m.Available,
		},
		Flag:            flag,
		CheckpointState: c.state.CheckpointState,
	}
}

// WriteOnlyFds takes a snapshot of files that are open (in writeonly) by process PID
// and outputs full paths. For concurrent processes (multithreaded) this can be dangerous and lead to
// weird race conditions (maybe).
// To avoid actually using ptrace (TODO NR) we loop through the openFds of the process and check the
// flags.

func (c *Client) WriteOnlyFds(openFds []*task.OpenFilesStat, pid int32) []string {
	fs := &afero.Afero{Fs: afero.NewOsFs()}
	var paths []string
	for _, fd := range openFds {
		info, err := fs.ReadFile(fmt.Sprintf("/proc/%s/fdinfo/%s", strconv.Itoa(int(pid)), strconv.FormatUint(fd.Fd, 10)))
		if err != nil {
			// c.Logger.Debug().Msgf("could not read fdinfo: %v", err)
			continue
		}

		lines := strings.Split(string(info), "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "flags:") {
				// parsing out flags from the line and converting it out of octal.
				// so converting flags: 0100002 -> 32770
				flags, err := strconv.ParseInt(strings.TrimSpace(line[6:]), 8, 0)
				if err != nil {
					// c.Logger.Debug().Msgf("could not parse flags: %v", err)
					continue
				}

				// bitwise compare flags with os.O_RDWR
				if int(flags)&os.O_RDWR != 0 || int(flags)&os.O_WRONLY != 0 {
					// gopsutil appends a (deleted) flag to the path sometimes, which I'm not fully sure of why yet
					// TODO NR - figure this out
					path := strings.Replace(fd.Path, " (deleted)", "", -1)
					paths = append(paths, path)
				}
			}
		}
	}
	return paths
}

// In the case where a program is execed from the daemon, we need to close FDs in common
// because without some complicated mechanics (like forking a shell process and then execing the task inside it)
// it's super difficult to fully detach a new process from Go.
// With ForkExec (see client-daemon.go) we get 90% of the way there, the last 10% is in finding the
// common FDs with the parent process and closing them.
// For an MVP/hack for now, just close the .pid file created by the daemon, which seems to be the problem child
func (c *Client) closeCommonFds(parentPID, childPID int32) error {
	parent, err := process.NewProcess(parentPID)
	if err != nil {
		return err
	}

	child, err := process.NewProcess(childPID)
	if err != nil {
		return err
	}

	parentFds, err := parent.OpenFiles()
	if err != nil {
		return err
	}

	childFds, err := child.OpenFiles()
	if err != nil {
		return err
	}

	for _, pfd := range parentFds {
		for _, cfd := range childFds {
			if pfd.Path == cfd.Path && strings.Contains(pfd.Path, ".pid") {
				// we have a match, close the FD
				c.Logger.Info().Msgf("closing common FD parent: %s, child: %s", pfd.Path, cfd.Path)
				err := syscall.Close(int(cfd.Fd))
				if err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func setupConnOptions(opts []nats.Option, logger *zerolog.Logger) []nats.Option {
	totalWait := 10 * time.Minute
	reconnectDelay := time.Second

	opts = append(opts, nats.ReconnectWait(reconnectDelay))
	opts = append(opts, nats.MaxReconnects(int(totalWait/reconnectDelay)))
	opts = append(opts, nats.DisconnectHandler(func(nc *nats.Conn) {
		logger.Info().Msgf("Disconnected: will attempt reconnects for %.0fm", totalWait.Minutes())
	}))
	opts = append(opts, nats.ReconnectHandler(func(nc *nats.Conn) {
		logger.Info().Msgf("Reconnected [%s]", nc.ConnectedUrl())
	}))
	opts = append(opts, nats.ClosedHandler(func(nc *nats.Conn) {
		logger.Info().Msgf("Exiting: %v", nc.LastError())
	}))
	return opts
}

// func (c *Client) startNATSService() {
// 	// create a subscription to NATS commands from the orchestrator first
// 	go c.subscribeToCommands(300)

// 	go c.publishStateContinuous(30)

// 	// listen for broadcast commands
// 	// subscribe to our broadcasters
// 	dumpCmdChn := c.channels.dumpCmdBroadcaster.Subscribe()
// 	restoreCmdChn := c.channels.restoreCmdBroadcaster.Subscribe()

// 	dir := c.config.SharedStorage.DumpStorageDir

// 	for {
// 		select {
// 		case <-dumpCmdChn:
// 			c.Logger.Info().Msg("received checkpoint command from NATS server")
// 			err := c.Dump(dir)
// 			if err != nil {
// 				c.Logger.Warn().Msgf("could not checkpoint process: %v", err)
// 				c.state.CheckpointState = cedana.CheckpointFailed
// 				c.publishStateOnce(c.getState(c.Process.PID))
// 			}
// 			c.state.CheckpointState = cedana.CheckpointSuccess
// 			c.publishStateOnce(c.getState(c.Process.PID))

// 		case cmd := <-restoreCmdChn:
// 			c.Logger.Info().Msg("received restore command from NATS server")
// 			pid, err := c.NatsRestore(&cmd, nil)
// 			if err != nil {
// 				c.Logger.Warn().Msgf("could not restore process: %v", err)
// 				c.state.CheckpointState = cedana.RestoreFailed
// 				c.publishStateOnce(c.getState(c.Process.PID))
// 			}
// 			c.state.CheckpointState = cedana.RestoreSuccess
// 			c.Process.PID = *pid
// 			c.publishStateOnce(c.getState(c.Process.PID))

// 		default:
// 			time.Sleep(1 * time.Second)
// 		}
// 	}
// }

func (c *Client) TryStartJob(taskPath *string) error {
	if taskPath == nil {
		// try config
		taskPath = &c.config.Client.Task
		c.Logger.Info().Msgf("no task provided, using task in config: %s", *taskPath)
	}
	// 5 attempts arbitrarily chosen - up to the orchestrator to send the correct task
	var err error
	for i := 0; i < 5; i++ {
		pid, err := c.RunTask(*taskPath)
		if err == nil {
			c.Logger.Info().Msgf("managing process with pid %d", pid)
			c.state.Flag = task.FlagEnum_JOB_RUNNING
			c.Process.PID = pid
			break
		} else {
			// enter a failure state, where we wait indefinitely for a command from NATS instead of
			// continuing
			c.Logger.Info().Msgf("failed to run task with error: %v, attempt %d", err, i+1)
			c.state.Flag = task.FlagEnum_JOB_STARTUP_FAILED
			// TODO BS: replace doom loop
			recoveryCmd := c.enterDoomLoop()
			taskPath = &recoveryCmd.UpdatedTask
		}
	}

	if err != nil {
		return err
	}

	return nil
}

// Deprecated
func (c *Client) RunTask(task string) (int32, error) {
	var pid int32

	if task == "" {
		return 0, fmt.Errorf("could not find task in config")
	}

	// need a more resilient/retriable way of doing this
	_, w, err := os.Pipe()
	if err != nil {
		return 0, err
	}

	nullFile, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		return 0, err
	}

	cmd := exec.Command("bash", "-c", task)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}

	cmd.Stdin = nullFile
	cmd.Stdout = w
	cmd.Stderr = w

	err = cmd.Start()
	if err != nil {
		return 0, err
	}

	pid = int32(cmd.Process.Pid)
	ppid := int32(os.Getpid())

	c.closeCommonFds(ppid, pid)

	// TODO BS: replace publishLogs with using grpc's bidirectional streaming.
	// if c.config.Client.ForwardLogs {
	// 	go c.publishLogs(r, w)
	// }

	return pid, nil
}

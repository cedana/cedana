package api

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/cedana/cedana/utils"
	retrier "github.com/eapache/go-resiliency/retrier"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/rs/zerolog"
	"github.com/shirou/gopsutil/v3/net"
	"github.com/shirou/gopsutil/v3/process"
	"github.com/spf13/afero"
	bolt "go.etcd.io/bbolt"
	"golang.org/x/time/rate"

	cedana "github.com/cedana/cedana/types"
)

// wrapper over filesystem, useful for mocking in tests
var AppFs = afero.NewOsFs()

type Client struct {
	CRIU *utils.Criu

	nc *nats.Conn
	js jetstream.JetStream

	logger *zerolog.Logger
	config *utils.Config

	channels *CommandChannels
	context  context.Context

	jobId  string
	selfId string

	// for dependency-injection of filesystems (useful for testing)
	fs *afero.Afero

	// external checkpoint store
	store utils.Store

	// db meta/state store
	db *DB
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

	// set up embedded key-value db
	conn, err := bolt.Open("/tmp/cedana.db", 0600, nil)
	if err != nil {
		logger.Fatal().Err(err).Msg("Could not open or create db")
		return nil, err
	}

	db := &DB{
		conn: conn,
	}

	return &Client{
		CRIU:     criu,
		logger:   &logger,
		config:   config,
		channels: channels,
		context:  context.Background(),
		fs:       fs,
		db:       db,
	}, nil
}

// Layers daemon capabilities onto client (adding nats, jetstream and jetstream contexts)
func (c *Client) AddNATS(selfID, jobID, authToken string) error {
	c.selfId = selfID
	c.jobId = jobID

	opts := []nats.Option{nats.Name(fmt.Sprintf("CEDANA_CLIENT_%s", selfID))}
	opts = setupConnOptions(opts, c.logger)
	opts = append(opts, nats.Token(authToken))

	var nc *nats.Conn
	var err error
	for i := 0; i < 5; i++ {
		nc, err = nats.Connect(c.config.Connection.NATSUrl, opts...)
		if err == nil {
			break
		}
		// reread config - I think there's a race that happens here with
		// read server overrides and the NATS connection.
		// TODO NR: should probably fix this
		c.config, _ = utils.InitConfig()
		c.logger.Warn().Msgf(
			"NATS connection failed (attempt %d/%d) with error: %v. Retrying...",
			i+1,
			5,
			err,
		)
		time.Sleep(30 * time.Second)
	}

	if err != nil {
		c.logger.Fatal().Err(err).Msg("Could not connect to NATS")
		return err
	}
	c.nc = nc

	// set up JetStream
	js, err := jetstream.New(nc)
	if err != nil {
		c.logger.Fatal().Err(err).Msg("Could not set up JetStream management interface")
		return err
	}
	c.js = js

	jsc, err := nc.JetStream()
	if err != nil {
		c.logger.Fatal().Err(err).Msg("Could not set up JetStream context")
		return err
	}

	natsStore := utils.NewNATSStore(c.logger, jsc, c.jobId)
	c.store = natsStore

	return nil
}

func (c *Client) CleanupClient() error {
	c.CRIU.Cleanup()
	c.db.Close()
	c.logger.Info().Msg("cleaning up client")
	return nil
}

func (c *Client) publishStateContinuous(rate int, id string) {
	c.logger.Info().Msgf("publishing state on CEDANA.%s.%s.state", c.jobId, c.selfId)
	ticker := time.NewTicker(time.Duration(rate) * time.Second)
	pid, err := c.db.GetPID(id)
	if err != nil {
		c.logger.Fatal().Err(err).Msg("could not get pid from in-memory store")
	}

	c.logger.Info().Msgf("pid: %d, task: %s", pid, c.config.Client.Task)
	// publish state continuously
	for range ticker.C {
		if pid != 0 {
			state, err := c.getState(pid)
			if err != nil {
				c.logger.Fatal().Err(err).Msg("could not get state")
			}
			c.publishStateOnce(state)
		}
	}
}

func (c *Client) publishLogs(r, w *os.File) {
	// we want to close this pipe prior to a checkpoint
	preDumpChn := c.channels.preDumpBroadcaster.Subscribe()

	// Limiting to 5 every 10 seconds
	limiter := rate.NewLimiter(rate.Every(10*time.Second), 5)

	buf := make([]byte, 4096)
	for {
		select {
		case <-preDumpChn:
			w.Close()
			r.Close()
		default:
			n, err := r.Read(buf)
			if err != nil {
				break
			}
			if limiter.Allow() {
				logEntry := &ClientLogs{
					Timestamp: time.Now().Local().Format(time.RFC3339),
					Source:    c.selfId,
					Level:     "INFO",
					Msg:       string(buf[:n]),
				}

				data, err := json.Marshal(logEntry)
				if err != nil {
					c.logger.Info().Msgf("could not marshal log entry: %v", err)
					continue
				}

				// we don't care about acks for logs right now
				_, err = c.js.PublishAsync(strings.Join([]string{"CEDANA", c.jobId, c.selfId, "logs"}, "."), data)
				if err != nil {
					c.logger.Info().Msgf("could not publish log entry: %v", err)
				}
			}
		}
	}
}

func (c *Client) publishStateOnce(state *cedana.ProcessState) {
	if state == nil {
		// we got no state, not necessarily an error condition - skip
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	data, err := json.Marshal(state)
	if err != nil {
		c.logger.Info().Msgf("could not marshal state: %v", err)
	}
	_, err = c.js.Publish(ctx, strings.Join([]string{"CEDANA", c.jobId, c.selfId, "state"}, "."), data)
	if err != nil {
		c.logger.Info().Msgf("could not publish state: %v", err)
	}

}

func (c *Client) subscribeToCommands(timeoutSec int, id string) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSec)*time.Second)
	defer cancel()

	r := retrier.New(retrier.ExponentialBackoff(10, 5*time.Second), nil)

	var cons jetstream.Consumer
	var err error
	err = r.Run(func() error {
		cons, err = c.js.CreateOrUpdateConsumer(ctx, "CEDANA", jetstream.ConsumerConfig{
			AckPolicy:     jetstream.AckExplicitPolicy,
			DeliverPolicy: jetstream.DeliverNewPolicy,
			FilterSubject: strings.Join([]string{"CEDANA", c.jobId, c.selfId, "commands"}, "."),
		})
		return err
	},
	)

	if err != nil {
		c.logger.Info().Msgf("could not subscribe to commands: %v", err)
	}

	pid, err := c.db.GetPID(id)
	if err != nil {
		c.logger.Fatal().Err(err).Msgf("could not get pid from id %s", id)
	}

	for {
		msg, err := cons.Fetch(1)
		if err != nil {
			c.logger.Info().Msgf("could not subscribe to commands: %v", err)
		}

		for msg := range msg.Messages() {
			c.logger.Debug().Msgf("received raw command: %v", msg)
			if msg != nil {
				var cmd cedana.ServerCommand
				err := json.Unmarshal(msg.Data(), &cmd)
				if err != nil {
					c.logger.Info().Msgf("could not unmarshal command: %v", err)
				}

				c.logger.Info().Msgf("received command: %v", cmd)
				if cmd.Command == "checkpoint" {
					msg.Ack()
					c.channels.dumpCmdBroadcaster.Broadcast(1)
					state, _ := c.getState(pid)
					c.publishStateOnce(state)
				} else if cmd.Command == "restore" {
					msg.Ack()
					c.channels.restoreCmdBroadcaster.Broadcast(cmd)
					state, _ := c.getState(pid)
					c.publishStateOnce(state)
				} else if cmd.Command == "retry" {
					msg.Ack()
					c.channels.retryCmdBroadcaster.Broadcast(cmd)
				} else {
					c.logger.Info().Msgf("received unknown command: %v", cmd)
					msg.Ack()
				}
			}
		}
	}
}

// Function called whenever we enter a failed state and need
// to wait for a command from the orchestrator to continue/unstuck the system.
// Ideally we can use this across the board whenever a case props up that requires
// orchestrator/external intervention.
// Takes a flag as input, which is used to craft a state to pass to NATS and waits
// for a signal to exit. Since go blocks until a signal is received, we use a channel.
func (c *Client) enterDoomLoop(state *cedana.ProcessState) *cedana.ServerCommand {
	retryChn := c.channels.retryCmdBroadcaster.Subscribe()
	c.publishStateOnce(state)
	for {
		select {
		case cmd := <-retryChn:
			c.logger.Info().Msgf("received recover command")
			return &cmd
		}
	}
}

// sets up subscribers for dump and restore commands
func (c *Client) timeTrack(start time.Time, name string) {
	elapsed := time.Since(start)
	c.logger.Debug().Msgf("%s took %s", name, elapsed)
}

// generateState preserves flags like checkpointStatus but updates
// processInfo
func (c *Client) generateState(pid int32) (*cedana.ProcessState, error) {

	if pid == 0 {
		return nil, nil
	}

	var oldState *cedana.ProcessState
	var state cedana.ProcessState

	err := c.db.conn.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("default"))
		v := b.Get([]byte(strconv.Itoa(int(pid))))

		err := json.Unmarshal(v, &oldState)
		if err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	if oldState != nil {
		// set to oldState, and just update parts of it
		state = *oldState
	}

	p, err := process.NewProcess(pid)
	if err != nil {
		c.logger.Info().Msgf("Could not instantiate new gopsutil process with error %v", err)
	}

	var openFiles []process.OpenFilesStat
	var writeOnlyFiles []string
	var openConnections []net.ConnectionStat

	if p != nil {
		openFiles, err = p.OpenFiles()
		if err != nil {
			// don't want to error out and break
			return nil, nil
		}
		// used for network barriers (TODO: NR)
		openConnections, err = p.Connections()
		if err != nil {
			return nil, nil
		}
		writeOnlyFiles = c.WriteOnlyFds(openFiles, pid)
	}

	memoryUsed, _ := p.MemoryPercent()
	isRunning, _ := p.IsRunning()

	// if the process is actually running, we don't care that
	// we're potentially overriding a failed flag here.
	// In the case of a restored/resuscitated process this is a good thing
	if isRunning {
		state.Flag = cedana.JobRunning
	}

	// this is the status as returned by gopsutil.
	// ideally we want more than this, or some parsing to happen from this end
	status, _ := p.Status()

	// ignore sending network for now, little complicated
	state.ProcessInfo = cedana.ProcessInfo{
		OpenFds:                openFiles,
		OpenWriteOnlyFilePaths: writeOnlyFiles,
		MemoryPercent:          memoryUsed,
		IsRunning:              isRunning,
		OpenConnections:        openConnections,
		Status:                 strings.Join(status, ""),
	}

	return &state, nil
}

// WriteOnlyFds takes a snapshot of files that are open (in writeonly) by process PID
// and outputs full paths. For concurrent processes (multithreaded) this can be dangerous and lead to
// weird race conditions (maybe).
// To avoid actually using ptrace (TODO NR) we loop through the openFds of the process and check the
// flags.
func (c *Client) WriteOnlyFds(openFds []process.OpenFilesStat, pid int32) []string {
	var paths []string
	for _, fd := range openFds {
		info, err := c.fs.ReadFile(fmt.Sprintf("/proc/%s/fdinfo/%s", strconv.Itoa(int(pid)), strconv.FormatUint(fd.Fd, 10)))
		if err != nil {
			c.logger.Debug().Msgf("could not read fdinfo: %v", err)
			continue
		}

		lines := strings.Split(string(info), "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "flags:") {
				// parsing out flags from the line and converting it out of octal.
				// so converting flags: 0100002 -> 32770
				flags, err := strconv.ParseInt(strings.TrimSpace(line[6:]), 8, 0)
				if err != nil {
					c.logger.Debug().Msgf("could not parse flags: %v", err)
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
				c.logger.Info().Msgf("closing common FD parent: %s, child: %s", pfd.Path, cfd.Path)
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

// start nats service for a single process
// todo NR - relation to job id?
func (c *Client) startNATSService(id string) {
	// create a subscription to NATS commands from the orchestrator first
	go c.subscribeToCommands(300, id)

	go c.publishStateContinuous(30, id)

	// listen for broadcast commands
	// subscribe to our broadcasters
	dumpCmdChn := c.channels.dumpCmdBroadcaster.Subscribe()
	restoreCmdChn := c.channels.restoreCmdBroadcaster.Subscribe()

	dir := c.config.SharedStorage.DumpStorageDir
	pid, err := c.db.GetPID(id)
	if err != nil {
		// TODO NR - should we be more resilient to this? can bolt fail transiently
		c.logger.Fatal().Err(err).Msg("could not get pid from database")
	}
	for {
		select {
		case <-dumpCmdChn:
			c.logger.Info().Msg("received checkpoint command from NATS server")
			state, _ := c.getState(pid)
			if err != nil {
				c.logger.Warn().Msgf("could not generate state: %v", err)
			}

			err := c.Dump(dir, pid)
			if err != nil {
				c.logger.Warn().Msgf("could not checkpoint process: %v", err)
				state.CheckpointState = cedana.CheckpointFailed
				c.publishStateOnce(state)
			}
			state.CheckpointState = cedana.CheckpointSuccess
			c.publishStateOnce(state)

		case cmd := <-restoreCmdChn:
			c.logger.Info().Msg("received restore command from NATS server")
			state, _ := c.getState(pid)
			if err != nil {
				c.logger.Warn().Msgf("could not generate state: %v", err)
			}

			// where does path come from? TODO NR

			pid, err := c.Restore(cmd.RestorePath)
			if err != nil {
				c.logger.Warn().Msgf("could not restore process: %v", err)
				state.CheckpointState = cedana.RestoreFailed
				c.publishStateOnce(state)
			}
			// get a new state using restored pid
			state, _ = c.getState(*pid)
			state.CheckpointState = cedana.RestoreSuccess
			c.publishStateOnce(state)

		default:
			time.Sleep(1 * time.Second)
		}
	}
}

func (c *Client) getState(pid int32) (*cedana.ProcessState, error) {
	state, err := c.generateState(pid)
	if err != nil {
		return nil, err
	}

	err = c.db.UpdateProcessStateWithPID(pid, state)
	if err != nil {
		return nil, err
	}

	return state, err
}

func (c *Client) TryStartJob(task *string, id string) error {
	if task == nil {
		// try config
		task = &c.config.Client.Task
		c.logger.Info().Msgf("no task provided, using task in config: %s", *task)
	}

	// 5 attempts arbitrarily chosen - up to the orchestrator to send the correct task
	var state cedana.ProcessState
	var err error
	for i := 0; i < 5; i++ {
		pid, err := c.RunTask(*task)
		if err == nil {
			c.logger.Info().Msgf("managing process with pid %d", pid)
			state.Flag = cedana.JobRunning
			state.PID = pid
			break
		} else {
			// enter a failure state, where we wait indefinitely for a command from NATS instead of
			// continuing
			c.logger.Info().Msgf("failed to run task with error: %v, attempt %d", err, i+1)
			state.Flag = cedana.JobStartupFailed
			recoveryCmd := c.enterDoomLoop(&state)
			task = &recoveryCmd.UpdatedTask
		}
	}

	if err != nil {
		return err
	}

	err = c.db.CreateOrUpdateCedanaProcess(id, &state)
	if err != nil {
		return err
	}

	return nil
}

func (c *Client) RunTask(task string) (int32, error) {
	var pid int32

	if task == "" {
		return 0, fmt.Errorf("could not find task in config")
	}

	// need a more resilient/retriable way of doing this
	r, w, err := os.Pipe()
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

	go func() {
		err := cmd.Wait()
		if err != nil {
			c.logger.Warn().Msgf("task failed: %v", err)
		}
	}()

	pid = int32(cmd.Process.Pid)
	ppid := int32(os.Getpid())

	c.closeCommonFds(ppid, pid)

	if c.config.Client.ForwardLogs {
		go c.publishLogs(r, w)
	}

	return pid, nil
}

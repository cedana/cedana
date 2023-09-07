package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/cedana/cedana/cedanarpc"
	"github.com/cedana/cedana/utils"
	retrier "github.com/eapache/go-resiliency/retrier"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/rs/zerolog"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/net"
	"github.com/shirou/gopsutil/v3/process"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"golang.org/x/time/rate"

	cedana "github.com/cedana/cedana/types"
)

// wrapper over filesystem, useful for mocking in tests
var AppFs = afero.NewOsFs()

type Client struct {
	CRIU *utils.Criu

	nc  *nats.Conn
	js  jetstream.JetStream
	jsc nats.JetStreamContext

	logger *zerolog.Logger
	config *utils.Config

	channels *CommandChannels
	context  context.Context
	Process  cedana.ProcessInfo

	jobId  string
	selfId string

	// a single big state glob
	state cedana.CedanaState

	// for dependency-injection of filesystems (useful for testing)
	fs *afero.Afero

	// checkpoint store
	store utils.Store

	handler *cedanarpc.CheckpointServiceHandler
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

var clientCommand = &cobra.Command{
	Use:   "client",
	Short: "Directly dump/restore a process or start a daemon",
	RunE: func(cmd *cobra.Command, args []string) error {
		return fmt.Errorf("error: must also specify dump, restore or daemon")
	},
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
		logger:   &logger,
		config:   config,
		channels: channels,
		context:  context.Background(),
		fs:       fs,
	}, nil
}

// Layers daemon capabilities onto client (adding nats, jetstream and jetstream contexts)
func (c *Client) AddDaemonLayer() error {
	// get ids. TODO NR: uuid verification
	// these should also be added to the config just in case
	// TODO NR: some code kicking around too to transfer b/ween stuff in config and stuff in env
	selfId, exists := os.LookupEnv("CEDANA_CLIENT_ID")
	if !exists {
		c.logger.Fatal().Msg("Could not find CEDANA_CLIENT_ID - something went wrong during instance creation")
	}
	c.selfId = selfId

	jobId, exists := os.LookupEnv("CEDANA_JOB_ID")
	if !exists {
		c.logger.Fatal().Msg("Could not find CEDANA_JOB_ID - something went wrong during instance creation")
	}
	c.jobId = jobId

	authToken, exists := os.LookupEnv("CEDANA_AUTH_TOKEN")
	if !exists {
		c.logger.Fatal().Msg("Could not find CEDANA_AUTH_TOKEN - something went wrong during instance creation")
	}

	// connect to NATS
	opts := []nats.Option{nats.Name(fmt.Sprintf("CEDANA_CLIENT_%s", selfId))}
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

	c.handler = cedanarpc.NewCheckpointServiceHandler(context.Background(), nc, c)

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

	// until market server is deployed, use NATS as a store
	natsStore := utils.NewNATSStore(c.logger, jsc, c.jobId)
	c.store = natsStore

	return nil
}

func (c *Client) cleanupClient() error {
	c.CRIU.Cleanup()
	c.logger.Info().Msg("cleaning up client")
	return nil
}

func (c *Client) publishStateContinuous(rate int) {
	c.logger.Info().Msgf("publishing state on CEDANA.%s.%s.state", c.jobId, c.selfId)
	ticker := time.NewTicker(time.Duration(rate) * time.Second)
	c.logger.Info().Msgf("pid: %d", c.Process.PID)
	// publish state continuously
	for range ticker.C {
		state := c.getState(c.Process.PID)
		c.publishStateOnce(state)
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

func (c *Client) publishStateOnce(state *cedana.CedanaState) {
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

func (c *Client) subscribeToCommands(timeoutSec int) {
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

	conctx, err := cons.Consume(c.handler.Handler)
	defer conctx.Stop()

	if err != nil {
		c.logger.Info().Msgf("could not subscribe to commands: %v", err)
	}

	for {
		// on timer, initiate fetch and wait until we timeout
		// waits until timeout
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
					state := c.getState(c.Process.PID)
					c.publishStateOnce(state)
				} else if cmd.Command == "restore" {
					msg.Ack()
					c.channels.restoreCmdBroadcaster.Broadcast(cmd)
					state := c.getState(c.Process.PID)
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

func (c *Client) Checkpoint(ctx context.Context, req *cedanarpc.CheckpointRequest) (*cedanarpc.StateResponse, error) {
	state := c.getState(c.Process.PID)
	res := &cedanarpc.StateResponse{
		JobID:    c.jobId,
		WorkerID: c.selfId,
		ClientInfo: &cedanarpc.ClientInfo{
			ID:              state.ClientInfo.Id,
			Hostname:        state.ClientInfo.Hostname,
			Platform:        state.ClientInfo.Platform,
			Os:              state.ClientInfo.OS,
			Uptime:          state.ClientInfo.Uptime,
			RemainingMemory: state.ClientInfo.RemainingMemory,
		},
		ProcessInfo: &cedanarpc.ProcessInfo{
			Pid:                     state.ProcessInfo.PID,
			AttachedToHardwareAccel: state.ProcessInfo.AttachedToHardwareAccel,
			OpenFds:                 make([]*cedanarpc.OpenFilesStat, len(state.ProcessInfo.OpenFds)),
			OpenWriteOnlyFilePaths:  make([]string, len(state.ProcessInfo.OpenWriteOnlyFilePaths)),
			OpenConnections:         make([]*cedanarpc.ConnectionStat, len(state.ProcessInfo.OpenConnections)),
			MemoryPercent:           state.ProcessInfo.MemoryPercent,
			IsRunning:               state.ProcessInfo.IsRunning,
			Status:                  state.ProcessInfo.Status,
		},
		CheckpointType:  cedanarpc.CheckpointType(cedanarpc.CheckpointType_value[string(state.CheckpointType)]),
		CheckpointState: cedanarpc.CheckpointState(cedanarpc.CheckpointState_value[string(state.CheckpointState)]),
		Flag:            cedanarpc.Flag(cedanarpc.Flag_value[string(state.Flag)]),
	}
	for i := 0; i < len(state.ProcessInfo.OpenFds); i++ {
		res.ProcessInfo.OpenFds[i].Path = state.ProcessInfo.OpenFds[i].Path
		res.ProcessInfo.OpenFds[i].Fd = state.ProcessInfo.OpenFds[i].Fd
	}
	copy(res.ProcessInfo.OpenWriteOnlyFilePaths, state.ProcessInfo.OpenWriteOnlyFilePaths)
	for i := 0; i < len(state.ProcessInfo.OpenConnections); i++ {
		res.ProcessInfo.OpenConnections[i].Fd = state.ProcessInfo.OpenConnections[i].Fd
		res.ProcessInfo.OpenConnections[i].Family = state.ProcessInfo.OpenConnections[i].Family
		res.ProcessInfo.OpenConnections[i].Type = state.ProcessInfo.OpenConnections[i].Type
		res.ProcessInfo.OpenConnections[i].LocalAddr = &cedanarpc.Addr{Ip: state.ProcessInfo.OpenConnections[i].Laddr.IP, Port: state.ProcessInfo.OpenConnections[i].Laddr.Port}
		res.ProcessInfo.OpenConnections[i].RemoteAddr = &cedanarpc.Addr{Ip: state.ProcessInfo.OpenConnections[i].Raddr.IP, Port: state.ProcessInfo.OpenConnections[i].Raddr.Port}
		res.ProcessInfo.OpenConnections[i].Status = state.ProcessInfo.OpenConnections[i].Status
		res.ProcessInfo.OpenConnections[i].Uids = make([]int32, len(state.ProcessInfo.OpenConnections[i].Uids))
		copy(res.ProcessInfo.OpenConnections[i].Uids, state.ProcessInfo.OpenConnections[i].Uids)
		res.ProcessInfo.OpenConnections[i].Pid = state.ProcessInfo.OpenConnections[i].Pid
	}

	return res, nil
}

// Function called whenever we enter a failed state and need
// to wait for a command from the orchestrator to continue/unstuck the system.
// Ideally we can use this across the board whenever a case props up that requires
// orchestrator/external intervention.
// Takes a flag as input, which is used to craft a state to pass to NATS and waits
// for a signal to exit. Since go blocks until a signal is received, we use a channel.
func (c *Client) enterDoomLoop() *cedana.ServerCommand {
	retryChn := c.channels.retryCmdBroadcaster.Subscribe()
	c.publishStateOnce(&c.state)
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

func (c *Client) getState(pid int32) *cedana.CedanaState {
	// inefficient - but unsure about race condition issues
	p, err := process.NewProcess(pid)
	if err != nil {
		c.logger.Info().Msgf("Could not instantiate new gopsutil process with error %v", err)
	}

	var openFiles []process.OpenFilesStat
	var writeOnlyFiles []string
	var openConnections []net.ConnectionStat
	var flag cedana.Flag

	if p != nil {
		openFiles, err = p.OpenFiles()
		if err != nil {
			// don't want to error out and break
			return nil
		}
		// used for network barriers (TODO: NR)
		openConnections, err = p.Connections()
		if err != nil {
			return nil
		}
		writeOnlyFiles = c.WriteOnlyFds(openFiles, pid)
	}

	memoryUsed, _ := p.MemoryPercent()
	isRunning, _ := p.IsRunning()

	// if the process is actually running, we don't care that
	// we're potentially overriding a failed flag here.
	// In the case of a restored/resuscitated process this is a good thing
	if isRunning {
		flag = cedana.JobRunning
	}

	// this is the status as returned by gopsutil.
	// ideally we want more than this, or some parsing to happen from this end
	status, _ := p.Status()

	m, _ := mem.VirtualMemory()
	h, _ := host.Info()

	// ignore sending network for now, little complicated
	return &cedana.CedanaState{
		ProcessInfo: cedana.ProcessInfo{
			PID:                    pid,
			OpenFds:                openFiles,
			OpenWriteOnlyFilePaths: writeOnlyFiles,
			MemoryPercent:          memoryUsed,
			IsRunning:              isRunning,
			OpenConnections:        openConnections,
			Status:                 strings.Join(status, ""),
		},
		ClientInfo: cedana.ClientInfo{
			Id:              c.selfId,
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

func init() {
	rootCmd.AddCommand(clientCommand)
}

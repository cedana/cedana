package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/checkpoint-restore/go-criu/v5"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/nravic/cedana/utils"
	"github.com/rs/zerolog"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/net"
	"github.com/shirou/gopsutil/v3/process"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"
)

// wrapper over filesystem, useful for mocking in tests
var AppFs = afero.NewOsFs()

type Client struct {
	CRIU *criu.Criu

	nc  *nats.Conn
	js  jetstream.JetStream
	jsc nats.JetStreamContext

	logger *zerolog.Logger
	config *utils.Config

	channels *CommandChannels
	context  context.Context
	process  ProcessInfo

	jobId  string
	selfId string

	state CedanaState // only ever modified at checkpoint/restore time

	// need to dependency inject a filesystem
	fs *afero.Afero
}

// CedanaState encapsulates a CRIU checkpoint and includes
// filesystem state for a full restore. Typically serialized and shot around
// over the wire.
type CedanaState struct {
	ClientInfo     ClientInfo     `json:"client_info" mapstructure:"client_info"`
	ProcessInfo    ProcessInfo    `json:"process_info" mapstructure:"process_info"`
	CheckpointType CheckpointType `json:"checkpoint_type" mapstructure:"checkpoint_type"`
	// either local or remote checkpoint path (url vs filesystem path)
	CheckpointPath string `json:"checkpoint_path" mapstructure:"checkpoint_path"`
	// process state at time of checkpoint
	CheckpointState CheckpointState `json:"checkpoint_state" mapstructure:"checkpoint_state"`
}

func (cs *CedanaState) SerializeToFolder(dir string) error {
	serialized, err := json.MarshalIndent(cs, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(dir, "checkpoint_state.json")
	file, err := os.Create(path)
	if err != nil {
		return err
	}

	defer file.Close()
	_, err = file.Write(serialized)
	return err
}

type Logs struct {
	Stdout string `mapstructure:"stdout"`
	Stderr string `mapstructure:"stderr"`
}

type CommandChannels struct {
	dump_command    chan int
	restore_command chan ServerCommand
}

// TODO: Until there's a shared library, we'll have to duplicate this struct

type ProcessInfo struct {
	PID                     int32                   `json:"pid" mapstructure:"pid"`
	AttachedToHardwareAccel bool                    `json:"attached_to_hardware_accel" mapstructure:"attached_to_hardware_accel"`
	OpenFds                 []process.OpenFilesStat `json:"open_fds" mapstructure:"open_fds"` // list of open FDs
	OpenWriteOnlyFilePaths  []string                `json:"open_write_only" mapstructure:"open_write_only"`
	OpenConnections         []net.ConnectionStat    `json:"open_connections" mapstructure:"open_connections"` // open network connections
	MemoryPercent           float32                 `json:"memory_percent" mapstructure:"memory_percent"`     // % of total RAM used
	IsRunning               bool                    `json:"is_running" mapstructure:"is_running"`
	Status                  string                  `json:"status" mapstructure:"status"`
}

type ClientInfo struct {
	Id              string `json:"id" mapstructure:"id"`
	Hostname        string `json:"hostname" mapstructure:"hostname"`
	Platform        string `json:"platform" mapstructure:"platform"`
	OS              string `json:"os" mapstructure:"os"`
	Uptime          uint64 `json:"uptime" mapstructure:"uptime"`
	RemainingMemory uint64 `json:"remaining_memory" mapstructure:"remaining_memory"`
}

type GPUInfo struct {
	Count            int       `json:"count" mapstructure:"count"`
	UtilizationRates []float64 `json:"utilization_rates" mapstructure:"utilization_rates"`
	PowerUsage       uint64    `json:"power_usage" mapstructure:"power_usage"`
}

type ServerCommand struct {
	Command     string      `json:"command" mapstructure:"command"`
	Heartbeat   bool        `json:"heartbeat" mapstructure:"heartbeat"`
	CedanaState CedanaState `json:"cedana_state" mapstructure:"cedana_state"`
}

type CheckpointType string
type CheckpointState string

const (
	CheckpointTypeNone    CheckpointType = "none"
	CheckpointTypeCRIU    CheckpointType = "criu"
	CheckpointTypePytorch CheckpointType = "pytorch"
)

const (
	CheckpointSuccess CheckpointState = "CHECKPOINTED"
	CheckpointFailed  CheckpointState = "CHECKPOINT_FAILED"
	RestoreSuccess    CheckpointState = "RESTORED"
	RestoreFailed     CheckpointState = "RESTORE_FAILED"
)

var clientCommand = &cobra.Command{
	Use:   "client",
	Short: "Directly dump/restore a process or start a daemon",
	RunE: func(cmd *cobra.Command, args []string) error {
		return fmt.Errorf("error: must also specify dump, restore or daemon")
	},
}

func instantiateClient() (*Client, error) {
	// instantiate logger
	logger := utils.GetLogger()

	c := criu.MakeCriu()
	_, err := c.GetCriuVersion()
	if err != nil {
		logger.Fatal().Err(err).Msg("Error checking CRIU version")
		return nil, err
	}
	// prepare client
	err = c.Prepare()
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
	dump_command := make(chan int)
	restore_command := make(chan ServerCommand)
	channels := &CommandChannels{dump_command, restore_command}

	// set up filesystem wrapper
	fs := &afero.Afero{Fs: AppFs}

	return &Client{
		CRIU:     c,
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
	c.jsc = jsc

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
	// publish state continuously
	for range ticker.C {
		c.publishStateOnce()
	}
}

func (c *Client) publishStateOnce() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	state := c.getState(c.process.PID)
	if state == nil {
		// we got no state, not necessarily an error condition - skip this iteration
		return
	}
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

	// FetchNoWait - only get latest
	cons, err := c.js.AddConsumer(ctx, "CEDANA", jetstream.ConsumerConfig{
		AckPolicy:     jetstream.AckExplicitPolicy,
		DeliverPolicy: jetstream.DeliverNewPolicy,
		FilterSubject: strings.Join([]string{"CEDANA", c.jobId, c.selfId, "commands"}, "."),
	})

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
				var cmd ServerCommand
				err := json.Unmarshal(msg.Data(), &cmd)
				if err != nil {
					c.logger.Info().Msgf("could not unmarshal command: %v", err)
				}

				c.logger.Info().Msgf("received command: %v", cmd)
				if cmd.Command == "checkpoint" {
					msg.Ack()
					c.channels.dump_command <- 1
					c.publishStateOnce()
				} else if cmd.Command == "restore" {
					msg.Ack()
					c.channels.restore_command <- cmd
					c.publishStateOnce()
				} else {
					c.logger.Info().Msgf("received unknown command: %v", cmd)
					msg.Ack()
				}
			}
		}
	}
}

// need to find a way to attach metadata here for checkpoint type?
func (c *Client) publishCheckpointFile(filepath string) error {
	// TODO: Bucket & KV needs to be set up as part of instantiation
	store, err := c.jsc.ObjectStore(strings.Join([]string{"CEDANA", c.jobId, "checkpoints"}, "_"))
	if err != nil {
		return err
	}

	info, err := store.PutFile(filepath)
	if err != nil {
		return err
	}

	c.logger.Info().Msgf("uploaded checkpoint file: %v", *info)

	return nil
}

func (c *Client) getCheckpointFile(bucketFilePath string) (*string, error) {
	store, err := c.jsc.ObjectStore(strings.Join([]string{"CEDANA", c.jobId, "checkpoints"}, "_"))
	if err != nil {
		return nil, err
	}

	downloadedFileName := "cedana_checkpoint.zip"

	err = store.GetFile(bucketFilePath, downloadedFileName)
	if err != nil {
		return nil, err
	}

	c.logger.Info().Msgf("downloaded checkpoint file: %s to %s", bucketFilePath, downloadedFileName)

	// verify file exists
	// TODO NR: checksum
	_, err = os.Stat(downloadedFileName)
	if err != nil {
		c.logger.Fatal().Msg("error downloading checkpoint file")
		return nil, err
	}

	return &downloadedFileName, nil
}

// sets up subscribers for dump and restore commands
func (c *Client) timeTrack(start time.Time, name string) {
	elapsed := time.Since(start)
	c.logger.Debug().Msgf("%s took %s", name, elapsed)
}

func (c *Client) getState(pid int32) *CedanaState {
	// inefficient - but unsure about race condition issues
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

	// this is the status as returned by gopsutil.
	// ideally we want more than this, or some parsing to happen from this end
	status, _ := p.Status()

	m, _ := mem.VirtualMemory()
	h, _ := host.Info()

	// ignore sending network for now, little complicated
	return &CedanaState{
		ProcessInfo: ProcessInfo{
			PID:                    pid,
			OpenFds:                openFiles,
			OpenWriteOnlyFilePaths: writeOnlyFiles,
			MemoryPercent:          memoryUsed,
			IsRunning:              isRunning,
			OpenConnections:        openConnections,
			Status:                 strings.Join(status, ""),
		},
		ClientInfo: ClientInfo{
			Id:              c.selfId,
			Hostname:        h.Hostname,
			Platform:        h.Platform,
			OS:              h.OS,
			Uptime:          h.Uptime,
			RemainingMemory: m.Available,
		},
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

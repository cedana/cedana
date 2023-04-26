package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/checkpoint-restore/go-criu"
	"github.com/nats-io/nats.go"
	"github.com/nravic/cedana/utils"
	"github.com/rs/zerolog"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/spf13/cobra"
)

type Client struct {
	CRIU     *criu.Criu
	nc       *nats.Conn
	js       nats.JetStreamContext
	logger   *zerolog.Logger
	config   *utils.Config
	channels *CommandChannels
	context  context.Context
	process  Process
	jobId    string
	selfId   string
}

// struct to hold logs from a process/Job
// assume this is good enough for now!
type Logs struct {
	Stdout string `mapstructure:"stdout"`
	Stderr string `mapstructure:"stderr"`
}

type JobInfo struct {
	logs    Logs          `mapstructure:"logs"`
	elapsed time.Duration `mapstructure:"elapsed"`
}

type CommandChannels struct {
	dump_command    chan int
	restore_command chan int
}

type Process struct {
	Pid int `json:"pid" mapstructure:"pid"`
	// cedana-context process state
	AttachedToHardwareAccel bool `json:"attached_to_hardware_accel" mapstructure:"attached_to_hardware_accel"`
}

// TODO: Until there's a shared library, we'll have to duplicate this struct
type ClientState struct {
	ProcessInfo Process    `json:"process_info" mapstructure:"process_info"`
	ClientInfo  ClientInfo `json:"client_info" mapstructure:"client_info"`
}

type ClientInfo struct {
	Id              string `json:"id" mapstructure:"id"`
	Hostname        string `json:"hostname" mapstructure:"hostname"`
	Platform        string `json: "platform" mapstructure:"platform"`
	OS              string `json: "os" mapstructure:"os"`
	Uptime          uint64 `json: "uptime" mapstructure:"uptime"`
	RemainingMemory uint64 `json: "remaining_memory" mapstructure:"remaining_memory"`
}

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

	// connect to NATS
	opts := []nats.Option{nats.Name(fmt.Sprintf("CEDANA_CLIENT_%s", config.Client.ID))}
	opts = setupConnOptions(opts, &logger)
	nc, err := nats.Connect(config.Connection.NATSUrl, opts...)
	if err != nil {
		logger.Fatal().Err(err).Msg("Could not connect to NATS")
	}
	js, err := nc.JetStream()
	if err != nil {
		logger.Fatal().Err(err).Msg("Could not set up JetStream context")
	}
	// set up channels for daemon to listen on
	dump_command := make(chan int)
	restore_command := make(chan int)
	channels := &CommandChannels{dump_command, restore_command}

	// get ids. TODO NR: uuid verification
	selfId, exists := os.LookupEnv("CEDANA_CLIENT_ID")
	if !exists {
		logger.Fatal().Msg("Could not find CEDANA_CLIENT_ID - something went wrong during instance creation")
	}
	jobId, exists := os.LookupEnv("CEDANA_JOB_ID")
	if !exists {
		logger.Fatal().Msg("Could not find CEDANA_JOB_ID - something went wrong during instance creation")
	}
	return &Client{
		CRIU:     c,
		nc:       nc,
		js:       js,
		logger:   &logger,
		config:   config,
		channels: channels,
		context:  context.Background(),
		selfId:   selfId,
		jobId:    jobId,
	}, nil
}

func (c *Client) cleanupClient() error {
	c.CRIU.Cleanup()
	c.nc.Close()
	c.logger.Info().Msg("cleaning up client")
	return nil
}

// gets and publishes state over ecNats
func (c *Client) publishState(timeoutSec int) {
	ticker := time.NewTicker(time.Duration(timeoutSec) * time.Second)
	// publish state continuously
	for {
		select {
		case <-ticker.C:
			state := c.getState(c.process.Pid)
			data, err := json.Marshal(state)
			if err != nil {
				// do nothing
				c.logger.Info().Msgf("could not marshal state: %v", err)
			}
			_, err = c.js.Publish(strings.Join([]string{"cedana", c.jobId, c.selfId, "state"}, "."), data)
			if err != nil {
				c.logger.Info().Msgf("could not publish state: %v", err)
			}
		default:
			// do nothing
		}
	}
}

func (c *Client) subscribeToCommands(timeoutMin int) {
	sub, err := c.js.Subscribe(strings.Join([]string{"cedana", c.jobId, c.selfId, "commands"}, "."), func(msg *nats.Msg) {
		if msg != nil {
			cmd := string(msg.Data)
			c.logger.Info().Msgf("received command: %v", msg)
			// TODO: NR - there's some weird escaping happening, debug later
			if cmd == "\"checkpoint\"" {
				c.logger.Info().Msgf("received checkpoint command")
				c.channels.dump_command <- 1
			} else if cmd == "\"restore\"" {
				c.logger.Info().Msgf("received restore command")
				c.channels.restore_command <- 1
			} else {
				c.logger.Info().Msgf("received unknown command: %s", cmd)
			}
		}
		msg.Ack()
	})
	if err != nil {
		c.logger.Info().Msgf("could not subscribe to commands: %v", err)
	}

	timeout := time.Duration(timeoutMin) * time.Minute
	ctx, cancel := context.WithTimeout(c.context, timeout)
	defer cancel()

	for {
		// msg handled by handler
		_, err := sub.NextMsgWithContext(ctx)
		if err != nil {
			c.logger.Info().Msgf("could not receive message: %v", err)
		}
		time.Sleep(time.Duration(timeoutMin) * time.Minute)
	}
}

// sets up subscribers for dump and restore commands
func (c *Client) timeTrack(start time.Time, name string) {
	elapsed := time.Since(start)
	c.logger.Debug().Msgf("%s took %s", name, elapsed)
}

func (c *Client) getState(pid int) *ClientState {

	m, _ := mem.VirtualMemory()
	h, _ := host.Info()

	// ignore sending network for now, little complicated
	data := &ClientState{
		ProcessInfo: Process{
			Pid: pid,
		},
		ClientInfo: ClientInfo{
			Id:              c.config.Client.ID,
			Hostname:        h.Hostname,
			Platform:        h.Platform,
			OS:              h.OS,
			Uptime:          h.Uptime,
			RemainingMemory: m.Available,
		},
	}
	return data
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

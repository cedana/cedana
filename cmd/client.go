package cmd

import (
	"context"
	"fmt"
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
	nc       *nats.EncodedConn // we want an encoded connection
	logger   *zerolog.Logger
	config   *utils.Config
	channels *CommandChannels
	context  context.Context
	process  Process
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
	pid int
	// cedana-context process state
	attachedToHardwareAccel bool
}

// TODO: Until there's a shared library, we'll have to duplicate this struct
type ClientState struct {
	ProcessInfo Process    `mapstructure:"process_info"`
	ClientInfo  ClientInfo `mapstructure:"client_info"`
}

type ClientInfo struct {
	Id              string `mapstructure:"id"`
	Hostname        string `mapstructure:"hostname"`
	Platform        string `mapstructure:"platform"`
	OS              string `mapstructure:"os"`
	Uptime          uint64 `mapstructure:"uptime"`
	RemainingMemory uint64 `mapstructure:"remaining_memory"`
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
	opts := []nats.Option{nats.Name(fmt.Sprintf("Cedana client %s", config.Client.ID))}
	opts = setupConnOptions(opts, &logger)
	nc, err := nats.Connect(config.Connection.NATSUrl, opts...)
	if err != nil {
		logger.Fatal().Err(err).Msg("Could not connect to NATS")
	}
	ecNats, err := nats.NewEncodedConn(nc, nats.JSON_ENCODER)
	if err != nil {
		logger.Fatal().Err(err).Msg("Could not create encoded connection to NATS")
	}
	// set up channels for daemon to listen on
	dump_command := make(chan int)
	restore_command := make(chan int)
	channels := &CommandChannels{dump_command, restore_command}
	return &Client{
		CRIU:     c,
		nc:       ecNats,
		logger:   &logger,
		config:   config,
		channels: channels,
		context:  context.Background(),
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
			data := c.getState(c.process.pid)
			err := c.nc.Publish(c.config.Client.ID+"_state", data)
			if err != nil {
				c.logger.Info().Msgf("could not publish state: %v", err)
			}
		default:
			// do nothing
		}
	}
}

func (c *Client) subscribeToCommands(timeoutMin int) {
	sub, err := c.nc.Conn.SubscribeSync(c.config.Client.ID + "_command")
	if err != nil {
		c.logger.Fatal().Err(err).Msg("could not subscribe to NATS checkpoint channel")
	}

	// sub.NextMsg blocks until a message is received, so this timeout
	// should be more transparent
	msg, err := sub.NextMsg(time.Duration(timeoutMin) * time.Minute)
	if err != nil {
		// not a fatal error, just exit this function.
		// we expect function to be run as a goroutine anyway
		c.logger.Info().Msgf("could not get next message: %v", err)
		return
	}
	if msg != nil {
		cmd := string(msg.Data)
		switch cmd {
		case "dump":
			c.logger.Info().Msgf("received checkpoint command")
			c.channels.dump_command <- 1
		case "restore":
			c.logger.Info().Msgf("received restore command")
			c.channels.restore_command <- 1
		default:
			c.logger.Info().Msgf("received unknown command: %s", cmd)
		}
	}
}

func (c *Client) ackCommand() {
	c.logger.Info().Msgf("acknowledging command")
	err := c.nc.Publish(c.config.Client.ID+"_commands_checkpoint_ack", "ack")
	if err != nil {
		c.logger.Info().Msgf("could not publish ack: %v", err)
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
			pid: pid,
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

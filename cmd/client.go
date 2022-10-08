package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/checkpoint-restore/go-criu"
	pb "github.com/nravic/cedana/rpc"
	"github.com/nravic/cedana/utils"
	"github.com/rs/zerolog"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type Client struct {
	CRIU          *criu.Criu
	rpcClient     pb.CedanaClient
	rpcConnection *grpc.ClientConn
	logger        *zerolog.Logger
	config        *utils.Config
	channels      *CommandChannels
}

type CommandChannels struct {
	dump_command    chan int
	restore_command chan int
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
	jsconfig, err := json.Marshal(config)
	if err != nil {
		logger.Debug().RawJSON("config loaded", jsconfig)
	}

	// TODO: think about concurrency
	// TODO: connection options??
	var opts []grpc.DialOption
	// TODO: Config with setup and transport credentials
	opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	conn, err := grpc.Dial("localhost:5000", opts...)
	if err != nil {
		logger.Fatal().Err(err).Msg("Could not connect to RPC server")
	}
	rpcClient := pb.NewCedanaClient(conn)

	// set up channels for daemon to listen on
	dump_command := make(chan int)
	restore_command := make(chan int)
	channels := &CommandChannels{dump_command, restore_command}
	return &Client{c, rpcClient, conn, &logger, config, channels}, nil
}

func (c *Client) cleanupClient() error {
	c.CRIU.Cleanup()
	c.rpcConnection.Close()
	c.logger.Info().Msg("cleaning up client")
	return nil
}

func (c *Client) registerRPCClient(pid int) {
	ctx, cancel := context.WithTimeout(context.TODO(), 10*time.Second)
	defer cancel()

	state := getState(pid)
	resp, err := c.rpcClient.RegisterClient(ctx, state)
	if err != nil {
		c.logger.Fatal().Msgf("client.RegisterClient failed: %v", err)
	}
	c.logger.Info().Msgf("Response from orchestrator: %v", resp)
}

func (c *Client) recordState() {
	state := getState(pid)
	stream, err := c.rpcClient.RecordState(context.TODO())
	if err != nil {
		c.logger.Fatal().Err(err).Msgf("Could not open stream to send state")
	}

	quit := make(chan struct{})
	ticker := time.NewTicker(5 * time.Second)

	go func() {
		for {
			select {
			case <-ticker.C:
				c.logger.Debug().Msgf("sending state: %v", state)
				err := stream.Send(state)
				if err != nil {
					c.logger.Fatal().Err(err).Msg("Error sending state to orchestrator")
				}
			case <-quit:
				ticker.Stop()
				return
			default:
			}
		}
	}()
}

func (c *Client) pollForCommand(pid int) {
	stream, _ := c.rpcClient.PollForCommand(context.TODO())
	c.logger.Debug().Msg("polling for command at ")

	waitc := make(chan struct{})
	ticker := time.NewTicker(5 * time.Second)

	go func() {
		for {
			c.logger.Debug().Msgf("polling for command at %v", time.Now())
			select {
			// send state every 5 seconds
			case <-ticker.C:
				state := getState(pid)
				c.logger.Debug().Msgf("Sent state %v:", state)
				stream.Send(state)
			// do nothing otherwise (don't block for loop)
			default:
			}

			// action w/ command
			resp, err := stream.Recv()
			c.logger.Info().Msg(resp.String())
			if err == io.EOF {
				// read done
				close(waitc)
				return
			}

			if resp == nil {
				// do nothing if we don't have a command yet
				return
			}

			if err != nil {
				c.logger.Fatal().Err(err).Msg("client.pollForCommand failed")
			}
			if resp.Checkpoint {
				c.logger.Info().Msgf("checkpoint command received: %v", resp)
				c.channels.dump_command <- 1
			}
			if resp.Restore {
				c.logger.Info().Msgf("restore command received: %v", resp)
				c.channels.restore_command <- 1
			}
		}
	}()
}

func (c *Client) timeTrack(start time.Time, name string) {
	elapsed := time.Since(start)
	c.logger.Debug().Msgf("%s took %s", name, elapsed)
}

func getState(pid int) *pb.ClientState {

	m, _ := mem.VirtualMemory()
	h, _ := host.Info()

	// ignore sending network for now, little complicated

	return &pb.ClientState{
		Timestamp: time.Now().Unix(),
		ProcessInfo: &pb.ProcessInfo{
			ProcessPid: uint32(pid),
		},
		ClientInfo: &pb.ClientInfo{
			RemainingMemory: int32(m.Free),
			Os:              h.OS,
			Platform:        h.Platform,
			Uptime:          uint32(h.Uptime),
		},
	}
}

func init() {
	rootCmd.AddCommand(clientCommand)
}

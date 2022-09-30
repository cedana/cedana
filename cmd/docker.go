package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/docker/docker/client"
	pb "github.com/nravic/cedana/rpc"
	"github.com/nravic/cedana/utils"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var dockerCmd = &cobra.Command{
	Use:   "docker",
	Short: "Directly checkpoint/restore a container or start a daemon",
	RunE: func(cmd *cobra.Command, args []string) error {
		return fmt.Errorf("error: must also specify dump, restore or daemon")
	},
}

func init() {
	clientCommand.AddCommand(dockerCmd)
}

type DockerClient struct {
	Docker        *client.Client // confusing, I know
	rpcClient     pb.CedanaClient
	rpcConnection *grpc.ClientConn
	logger        *zerolog.Logger
	config        *utils.Config
	channels      *CommandChannels
}

func instantiateDockerClient() (*DockerClient, error) {
	// use a docker client instead of CRIU directly
	logger := utils.GetLogger()

	cli, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		logger.Fatal().Err(err).Msg("Error instantiating docker client")
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

	var opts []grpc.DialOption
	// TODO
	opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	conn, err := grpc.Dial(fmt.Sprintf("%v:%d", config.Connection.ServerAddr, config.Connection.ServerPort), opts...)
	if err != nil {
		logger.Fatal().Err(err).Msg("Could not connect to RPC server")
	}
	rpcClient := pb.NewCedanaClient(conn)

	dump_command := make(chan int)
	restore_command := make(chan int)
	channels := &CommandChannels{dump_command, restore_command}

	return &DockerClient{cli, rpcClient, conn, &logger, config, channels}, nil
}

func (c *DockerClient) pollForCommand(pid int) {
	stream, _ := c.rpcClient.PollForCommand(context.Background())
	waitc := make(chan struct{})
	go func() {
		for {
			resp, err := stream.Recv()
			if err == io.EOF {
				// read done
				close(waitc)
				return
			}
			if err != nil {
				c.logger.Fatal().Msgf("client.pollForCommand failed: %v", err)
			}
			if resp.Checkpoint {
				c.channels.dump_command <- 1
			}
			if resp.Restore {
				c.channels.restore_command <- 1
			}
			stream.Send(getState(pid))
		}
	}()
}

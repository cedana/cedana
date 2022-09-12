package cmd

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/checkpoint-restore/go-criu"
	pb "github.com/nravic/cedana-client/rpc"
	"github.com/nravic/cedana-client/utils"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type Client struct {
	CRIU          *criu.Criu
	rpcClient     *pb.CedanaClient
	rpcConnection *grpc.ClientConn
	logger        *zerolog.Logger
}

var clientCommand = &cobra.Command{
	Use:   "client",
	Short: "Directly dump/restore a checkpoint or start a daemon",
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

	return &Client{c, &rpcClient, conn, &logger}, err
}

func (c *Client) cleanupClient() error {
	c.CRIU.Cleanup()
	c.rpcConnection.Close()
	c.logger.Info().Msg("Cleaning up client")
	// TODO: should be deferrable maybe?
	return nil
}

func registerRPCClient(client pb.CedanaClient) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	state := getState()
	params, err := client.RegisterClient(ctx, state)
	if err != nil {
		log.Fatalf("client.RegisterClient failed: %v", err)
	}
	// take params, marshal it
	log.Println(params)
}

// record and send state
func runRecordState(client pb.CedanaClient) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	stream, err := client.RecordState(ctx)
	if err != nil {
		log.Fatalf("client.RecordState failed: %v", err)
	}

	// TODO - spawn a goroutine here
	for i := 1; i < 10; i++ {
		stream.Send(getState())
	}
	reply, err := stream.CloseAndRecv()
	if err != nil {
		log.Fatalf("client.RecordState failed: %v", err)
	}
	log.Printf("Response: %v", reply)
}

// TODO: Send out better state
func getState() *pb.ClientState {
	// TODO: Populate w/ process data and other stuff in the RPC definition

	return &pb.ClientState{
		Timestamp: time.Now().Unix(),
	}
}

func init() {
	rootCmd.AddCommand(clientCommand)
}

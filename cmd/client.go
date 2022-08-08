package cmd

import (
	"context"
	"log"
	"time"

	"github.com/checkpoint-restore/go-criu"
	pb "github.com/nravic/oort/rpc"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// TODO: (Big one) - We destroy the client object (or rather let the object get garbage collected after cleanup)

type Client struct {
	CRIU          *criu.Criu
	rpcClient     *pb.OortClient
	rpcConnection *grpc.ClientConn
}

var clientCommand = &cobra.Command{
	Use:   "client",
	Short: "Use with dump or restore (dump first obviously)",
	RunE: func(cmd *cobra.Command, args []string) error {
		return nil
	},
}

func instantiateClient() (*Client, error) {
	c := criu.MakeCriu()
	// check if version is good, otherwise get out
	_, err := c.GetCriuVersion()
	if err != nil {
		log.Fatal("Error checking CRIU version!", err)
		return nil, err
	}
	// prepare client
	err = c.Prepare()
	if err != nil {
		log.Fatal("Error preparing CRIU client", err)
		return nil, err
	}
	// TODO: think about concurrency
	// TODO: connection options??
	var opts []grpc.DialOption
	// TODO: Config with setup and transport credentials
	opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	conn, err := grpc.Dial("localhost:5000", opts...)
	if err != nil {
		log.Fatalf("Could not connect to RPC server %v", err)
	}
	rpcClient := pb.NewOortClient(conn)
	return &Client{c, &rpcClient, conn}, err
}

func (c *Client) cleanupClient() error {
	c.CRIU.Cleanup()
	c.rpcConnection.Close()
	// TODO: should be deferrable maybe?
	return nil
}

// Register client with RPC server
func registerRPCClient(client pb.OortClient) {
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
func runRecordState(client pb.OortClient) {
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

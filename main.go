package cmd

import (
	"context"
	"log"
	"time"

	pb "github.com/nravic/oort/rpc"
	"google.golang.org/grpc"
)

func initializeClient(client pb.OortClient) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	params, err := client.RegisterClient(ctx)
}

func runRecordState(client pb.OortClient) {
	// get state and send to server on stream
}

func getState() *pb.ClientState {
	return &pb.ClientState{
		Timestamp: time.Now().Unix(),
	}
	// garbage data for now
}

func main() {
	var opts []grpc.DialOption
	conn, err := grpc.Dial("localhost:5000", opts...)
	if err != nil {
		log.Fatalf("Could not connect to RPC server %v", err)
	}

	defer conn.Close()
	client := pb.NewOortClient(conn)

}

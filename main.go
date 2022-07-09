package main

import (
	"context"
	"log"
	"time"

	pb "github.com/nravic/oort/rpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func initializeClient(client pb.OortClient) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	state := getState()
	params, err := client.RegisterClient(ctx, state)
	if err != nil {
		log.Fatalf("client.RegisterClient failed: %v", err)
	}

	log.Println(params)
}

func runRecordState(client pb.OortClient) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	stream, err := client.RecordState(ctx)
	if err != nil {
		log.Fatalf("client.RecordState failed: %v", err)
	}
	for i := 1; i < 10; i++ {
		stream.Send(&pb.ClientState{
			Timestamp: time.Now().Unix(),
		})
	}
	reply, err := stream.CloseAndRecv()
	if err != nil {
		log.Fatalf("client.RecordState failed: %v", err)
	}
	log.Printf("Response: %v", reply)
}

func getState() *pb.ClientState {
	return &pb.ClientState{
		Timestamp: time.Now().Unix(),
	}
	// garbage data for now
}

func main() {
	var opts []grpc.DialOption
	opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	conn, err := grpc.Dial("localhost:5000", opts...)
	if err != nil {
		log.Fatalf("Could not connect to RPC server %v", err)
	}

	defer conn.Close()
	client := pb.NewOortClient(conn)

	initializeClient(client)
	runRecordState(client)

}

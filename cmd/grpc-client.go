package cmd

import (
	"log"

	pb "github.com/nravic/oort/rpc"
	"google.golang.org/grpc"
)

func main() {
	var opts []grpc.DialOption
	conn, err := grpc.Dial("localhost:5000", opts...)
	if err != nil {
		log.Fatalf("Could not connect to RPC server %v", err)
	}

	defer conn.Close()
	client := pb.NewOortClient(conn)

}

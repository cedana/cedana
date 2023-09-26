package server

import (
	"context"
	"net"

	checkpoint "github.com/cedana/cedana/api/checkpoint/server"
	"google.golang.org/grpc"
)

type dumpServer struct {
	checkpoint.UnimplementedDumpServiceServer
}

func (s *dumpServer) Dump(context.Context, *checkpoint.DumpArgs) (*checkpoint.DumpResp, error) {
	return &checkpoint.DumpResp{
		Error: "not implemented",
	}, nil
}

func Start() {
	lis, err := net.Listen("tcp", ":8080")
	if err != nil {
		panic(err)
	}
	grpcServer := grpc.NewServer()

	service := &dumpServer{}

	checkpoint.RegisterDumpServiceServer(grpcServer, service)
	if err := grpcServer.Serve(lis); err != nil {
		panic(err)
	}
	defer lis.Close()
}

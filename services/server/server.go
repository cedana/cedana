package server

import (
	"context"
	"net"

	"github.com/cedana/cedana/api/services/checkpoint"
	"google.golang.org/grpc"
)

type service struct {
	checkpoint.UnimplementedDumpServiceServer
}

func (s *service) Dump(context.Context, *checkpoint.DumpArgs) (*checkpoint.DumpResp, error) {
	return &checkpoint.DumpResp{
		Error: "not implemented",
	}, nil
}

type Server struct {
	grpcServer *grpc.Server
	Lis        *net.Listener
}

type GrpcService interface {
	Register(*grpc.Server) error
}

func (s *Server) New() (*Server, error) {

	var (
		grpcServer = grpc.NewServer()
	)

	service := &service{}

	checkpoint.RegisterDumpServiceServer(grpcServer, service)

	return &Server{
		grpcServer: grpcServer,
	}, nil
}

func (s *Server) ServeGRPC(l net.Listener) error {
	return s.grpcServer.Serve(l)
}

func (s *Server) Start() {
	lis, err := net.Listen("tcp", ":8080")
	if err != nil {
		panic(err)
	}
	s.Lis = &lis
}

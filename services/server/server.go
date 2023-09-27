package server

import (
	"context"
	"net"

	"github.com/cedana/cedana/api"
	"github.com/cedana/cedana/api/services/checkpoint"
	"google.golang.org/grpc"
)

type service struct {
	checkpoint.UnimplementedDumpServiceServer
}

func addGRPC() (*Server, error) {
	server := &Server{}
	server.New()
	return server, nil
}

func StartGRPCServer() error {
	srv, err := addGRPC()
	if err != nil {
		return err
	}

	go srv.start()

	go srv.serveGRPC(*srv.Lis)

	return nil
}

func (s *service) Dump(ctx context.Context, args *checkpoint.DumpArgs) (*checkpoint.DumpResp, error) {
	client := api.Client{}
	client.Dump(args.Dir)
	return &checkpoint.DumpResp{
		Error: "not implemented",
	}, nil
}

type Server struct {
	client     *api.Client
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

	s.client = &api.Client{}

	return &Server{
		grpcServer: grpcServer,
	}, nil
}

func (s *Server) serveGRPC(l net.Listener) error {
	return s.grpcServer.Serve(l)
}

func (s *Server) start() {
	lis, err := net.Listen("tcp", ":8080")
	if err != nil {
		panic(err)
	}
	s.Lis = &lis
}

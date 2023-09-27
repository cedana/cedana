package server

import (
	"context"
	"net"
	"sync"

	"github.com/cedana/cedana/api"
	task "github.com/cedana/cedana/api/services/task"
	"github.com/cedana/cedana/types"
	"google.golang.org/grpc"
)

// Unused for now...
type GrpcService interface {
	Register(*grpc.Server) error
}

type service struct {
	Client *api.Client
	task.UnimplementedTaskServiceServer
}

func (s *service) Dump(ctx context.Context, args *task.DumpArgs) (*task.DumpResp, error) {
	client := s.Client
	err := client.Dump(args.Dir)

	return &task.DumpResp{
		Error: err.Error(),
	}, err
}

func (s *service) Restore(ctx context.Context, args *task.RestoreArgs) (*task.RestoreResp, error) {
	client := s.Client
	cmd := &types.ServerCommand{}
	pid, err := client.Restore(cmd, &args.Path)

	return &task.RestoreResp{
		Error:  err.Error(),
		NewPID: *pid,
	}, err
}

func (s *service) StartTask(ctx context.Context, args *task.StartTaskArgs) (*task.StartTaskResp, error) {
	client := s.Client
	_, err := client.RunTask(args.Task)

	return &task.StartTaskResp{
		Error: err.Error(),
	}, err
}

type Server struct {
	grpcServer *grpc.Server
	Lis        net.Listener
}

func (s *Server) New() (*grpc.Server, error) {

	grpcServer := grpc.NewServer()

	client := &api.Client{}

	service := &service{
		Client: client,
	}

	task.RegisterTaskServiceServer(grpcServer, service)

	return grpcServer, nil
}

func (s *Server) serveGRPC(l net.Listener) error {
	return s.grpcServer.Serve(l)
}

func (s *Server) start() {
	lis, err := net.Listen("tcp", ":8080")
	if err != nil {
		panic(err)
	}
	s.Lis = lis
}

func addGRPC() (*Server, error) {
	server := &Server{}
	srv, err := server.New()
	server.grpcServer = srv
	if err != nil {
		return nil, err
	}
	return server, nil
}

func StartGRPCServer() error {
	var wg sync.WaitGroup

	srv, err := addGRPC()
	if err != nil {
		return err
	}

	go srv.start()

	go srv.serveGRPC(srv.Lis)

	wg.Add(2)

	wg.Wait()

	return nil
}

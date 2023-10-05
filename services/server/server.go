package server

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/cedana/cedana/api"
	task "github.com/cedana/cedana/api/services/task"
	"github.com/cedana/cedana/utils"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

// Unused for now...
type GrpcService interface {
	Register(*grpc.Server) error
}

type UploadResponse struct {
	UploadID  string `json:"upload_id"`
	PartSize  int    `json:"part_size"`
	PartCount int    `json:"part_count"`
}

type service struct {
	Client *api.Client
	task.UnimplementedTaskServiceServer
}

func (s *service) Dump(ctx context.Context, args *task.DumpArgs) (*task.DumpResp, error) {

	// var zipFileSize uint64
	cfg := utils.Config{}
	store := utils.NewCedanaStore(&cfg)

	client := s.Client
	client.Process.PID = args.PID

	err := client.Dump(args.Dir)

	if err != nil {
		return nil, err
	}

	file, err := os.Open(client.CheckpointDir + ".zip")
	if err != nil {
		return nil, err
	}
	defer file.Close()

	// Get the file size
	fileInfo, err := file.Stat()
	if err != nil {
		return nil, err
	}

	// Get the size
	size := fileInfo.Size()

	// zipFileSize += 4096

	checkpointFullSize := int64(size)

	multipartCheckpointResp, cid, err := store.CreateMultiPartUpload(checkpointFullSize)
	if err != nil {
		return nil, err
	}

	err = store.StartMultiPartUpload(cid, &multipartCheckpointResp, client.CheckpointDir)
	if err != nil {
		return nil, err
	}

	err = store.CompleteMultiPartUpload(multipartCheckpointResp, cid)
	if err != nil {
		return nil, err
	}

	return &task.DumpResp{
		Error: fmt.Sprintf("Dumped process %d to %s, multipart checkpoint id: %s", args.PID, args.Dir, multipartCheckpointResp.UploadID),
	}, nil
}

func (s *service) Restore(ctx context.Context, args *task.RestoreArgs) (*task.RestoreResp, error) {
	client := s.Client

	pid, err := client.Restore(args)

	return &task.RestoreResp{
		Error:  err.Error(),
		NewPID: *pid,
	}, err
}

// Not needed I do not think...
func (s *service) StartTask(ctx context.Context, args *task.StartTaskArgs) (*task.StartTaskResp, error) {
	client := s.Client
	_, err := client.RunTask(args.Task)

	return &task.StartTaskResp{
		Error: err.Error(),
	}, err
}

func (s *service) LogStreaming(stream task.TaskService_LogStreamingServer) error {
	for {
		// Here we can do something with LogStreamingArgs
		_, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		stream.Send(&task.LogStreamingResp{Status: "OK"})
	}
}

type Server struct {
	grpcServer *grpc.Server
	Lis        net.Listener
}

func (s *Server) New() (*grpc.Server, error) {

	grpcServer := grpc.NewServer()

	client, err := api.InstantiateClient()
	if err != nil {
		return nil, err
	}

	service := &service{
		Client: client,
	}

	task.RegisterTaskServiceServer(grpcServer, service)

	reflection.Register(grpcServer)

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

	startCh := make(chan struct{})
	wg.Add(2)
	go func() {
		defer wg.Done()
		srv.start()
		close(startCh) // Signal that the server has started
	}()

	go func() {
		<-startCh // Wait for the server to start
		srv.serveGRPC(srv.Lis)
	}()

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt, syscall.SIGTERM)

	wg.Wait()

	// Cleanup here

	return nil
}

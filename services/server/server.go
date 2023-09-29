package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/cedana/cedana/api"
	task "github.com/cedana/cedana/api/services/task"
	"github.com/cedana/cedana/types"
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

func createMultiPartUpload(fullSize int64) (UploadResponse, error) {
	var uploadResp UploadResponse

	data := struct {
		Name     string `json:"name"`
		FullSize int64  `json:"full_size"`
		PartSize int    `json:"part_size"`
	}{
		// TODO BS Need to get TaskID properly...
		Name:     "test",
		FullSize: fullSize,
		PartSize: 0,
	}

	payload, err := json.Marshal(data)
	if err != nil {
		return uploadResp, err
	}

	httpClient := &http.Client{}
	url := os.Getenv("CHECKPOINT_SERVICE_URL") + "/checkpoint/6291dc64-289f-4744-9aa6-2a382b0a9a30/upload"

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(payload))
	if err != nil {
		return uploadResp, err
	}

	req.Header.Set("Content-Type", "application/json")

	req.Header.Set("Authorization", "Bearer brandonsmith")

	resp, err := httpClient.Do(req)
	if err != nil {
		return uploadResp, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return uploadResp, err
	}

	// Parse the JSON response into the struct
	if err := json.Unmarshal(respBody, &uploadResp); err != nil {
		fmt.Println("Error parsing JSON response:", err)
		return uploadResp, err
	}

	return uploadResp, nil
}
func startMultiPartUpload(uploadResp *UploadResponse) error {
	// TODO BS: implement

	filePath := "./part2.bin"
	file, err := os.Open(filePath)
	if err != nil {
		fmt.Println("Error opening file:", err)
		return err
	}
	defer file.Close()

	// Create a buffer to read the file data into
	buffer := new(bytes.Buffer)
	_, err = buffer.ReadFrom(file)
	if err != nil {
		fmt.Println("Error reading file data:", err)
		return err
	}

	httpClient := &http.Client{}
	url := os.Getenv("CHECKPOINT_SERVICE_URL") + "/checkpoint/6291dc64-289f-4744-9aa6-2a382b0a9a30/upload"

	req, err := http.NewRequest("POST", url, buffer)
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/octect-stream")
	req.Header.Set("Transfer-Encoding", "chunked")
	req.Header.Set("Authorization", "Bearer brandonsmith")

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	fmt.Printf("Response: %s\n", respBody)

	return nil
}
func completeMultiPartUpload() {
	// TODO BS: implement
}

type service struct {
	Client *api.Client
	task.UnimplementedTaskServiceServer
}

func (s *service) Dump(ctx context.Context, args *task.DumpArgs) (*task.DumpResp, error) {
	client := s.Client
	client.Process.PID = args.PID

	err := client.Dump(args.Dir)

	if err != nil {
		return nil, err
	}

	checkpointInfo, err := os.Stat(client.CheckpointDir)
	if err != nil {
		return nil, err
	}
	checkpointFullSize := checkpointInfo.Size()

	err = createMultiPartUpload(checkpointFullSize)

	if err != nil {
		return nil, err
	}

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

	// Start the gRPC server
	srv.start()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for srv.Lis == nil {
			// Wait until the server's listener is initialized
			time.Sleep(time.Millisecond * 100)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		srv.serveGRPC(srv.Lis)
	}()

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt, syscall.SIGTERM)

	wg.Wait()

	// Cleanup here

	return nil
}

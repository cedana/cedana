package server

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/cedana/cedana/api"
	task "github.com/cedana/cedana/api/services/task"
	"github.com/cedana/cedana/utils"
	"github.com/shirou/gopsutil/v3/process"
	"golang.org/x/time/rate"
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
	Client            *api.Client
	ClientLogStream   task.TaskService_LogStreamingServer
	ClientStateStream task.TaskService_ClientStateStreamingServer
	r                 *os.File
	w                 *os.File
	task.UnimplementedTaskServiceServer
}

func (s *service) Dump(ctx context.Context, args *task.DumpArgs) (*task.DumpResp, error) {
	// Close before dumping
	s.r.Close()
	s.w.Close()
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

func (s *service) publishStateContinous(rate int) {
	s.Client.Logger.Info().Msgf("pid: %d", s.Client.Process.PID)
	ticker := time.NewTicker(time.Duration(rate) * time.Second)
	for range ticker.C {
		if s.Client.Process.PID != 0 {
			args := &task.ClientStateStreamingArgs{}

			if err := s.ClientStateStream.Send(args); err != nil {
				log.Printf("Error sending LogStreamingArgs to client: %v", err)
				return
			}
		}
	}
}

// This is for the orchestrator
func (s *service) LogStreaming(stream task.TaskService_LogStreamingServer) error {
	limiter := rate.NewLimiter(rate.Every(10*time.Second), 5)
	buf := make([]byte, 4096)

	for {
		select {
		case <-stream.Context().Done():
			return nil // Client disconnected
		default:
			n, err := s.r.Read(buf)
			if err != nil {
				break
			}
			if limiter.Allow() {
				// TODO BS Needs implementation
				response := &task.LogStreamingArgs{
					Timestamp: time.Now().Local().Format(time.RFC3339),
					Source:    "Not implemented",
					Level:     "INFO",
					Msg:       string(buf[:n]),
				}
				if err := stream.Send(response); err != nil {
					return err
				}
			}
		}
	}
}

// This is for the orchestrator
func (s *service) ClientStateStreaming(stream task.TaskService_ClientStateStreamingServer) error {
	// Store the client's stream when it connects.
	s.ClientStateStream = stream

	go s.publishStateContinous(30)

	for {
		// Here we can do something with LogStreamingResp
		_, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}

		if s.ClientStateStream != nil {

			args := &task.ClientStateStreamingArgs{}
			if err := s.ClientStateStream.Send(args); err != nil {
				return err
			}
		}
	}
}

func closeCommonFds(parentPID, childPID int32) error {
	parent, err := process.NewProcess(parentPID)
	if err != nil {
		return err
	}

	child, err := process.NewProcess(childPID)
	if err != nil {
		return err
	}

	parentFds, err := parent.OpenFiles()
	if err != nil {
		return err
	}

	childFds, err := child.OpenFiles()
	if err != nil {
		return err
	}

	for _, pfd := range parentFds {
		for _, cfd := range childFds {
			if pfd.Path == cfd.Path && strings.Contains(pfd.Path, ".pid") {
				// we have a match, close the FD
				err := syscall.Close(int(cfd.Fd))
				if err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (s *service) runTask(task string) (int32, error) {
	var pid int32

	if task == "" {
		return 0, fmt.Errorf("could not find task in config")
	}

	// need a more resilient/retriable way of doing this
	r, w, err := os.Pipe()
	s.r = r
	s.w = w
	if err != nil {
		return 0, err
	}

	nullFile, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		return 0, err
	}

	cmd := exec.Command("bash", "-c", task)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}

	cmd.Stdin = nullFile
	cmd.Stdout = w
	cmd.Stderr = w

	err = cmd.Start()
	if err != nil {
		return 0, err
	}

	pid = int32(cmd.Process.Pid)
	ppid := int32(os.Getpid())

	closeCommonFds(ppid, pid)
	return pid, nil
}

func (s *service) StartTask(ctx context.Context, args *task.StartTaskArgs) (*task.StartTaskResp, error) {

	_, err := s.runTask(args.Task)
	if err != nil {
		return nil, err
	}

	return &task.StartTaskResp{
		Error: "",
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

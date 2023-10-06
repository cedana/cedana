package api

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"
	"time"

	task "github.com/cedana/cedana/api/services/task"
	"github.com/cedana/cedana/container"
	"github.com/cedana/cedana/utils"
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
	Client            *Client
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
	s.Client.Process = &task.ProcessInfo{}

	s.Client.Process.PID = args.PID

	err := s.Client.Dump(args.Dir, args.PID)
	if err != nil {
		return nil, err
	}

	file, err := os.Open(s.Client.CheckpointDir + ".zip")
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

	err = store.StartMultiPartUpload(cid, &multipartCheckpointResp, s.Client.CheckpointDir)
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

func (s *service) ContainerDump(ctx context.Context, args *task.ContainerDumpArgs) (*task.ContainerDumpResp, error) {
	err := s.Client.ContainerDump(args.Ref, args.ContainerId)
	if err != nil {
		return nil, err
	}
	return &task.ContainerDumpResp{}, nil
}

func (s *service) ContainerRestore(ctx context.Context, args *task.ContainerRestoreArgs) (*task.ContainerRestoreResp, error) {
	err := s.Client.ContainerRestore(args.ImgPath, args.ContainerId)
	if err != nil {
		return nil, err
	}
	return &task.ContainerRestoreResp{}, nil
}

func (s *service) RuncDump(ctx context.Context, args *task.RuncDumpArgs) (*task.RuncDumpResp, error) {
	// TODO BS: This is a hack for now
	criuOpts := &container.CriuOpts{
		ImagesDirectory: args.CriuOpts.ImagesDirectory,
		WorkDirectory:   args.CriuOpts.WorkDirectory,
		LeaveRunning:    true,
		TcpEstablished:  false,
	}

	err := s.Client.RuncDump(args.Root, args.ContainerId, criuOpts)
	if err != nil {
		return nil, err
	}

	return &task.RuncDumpResp{}, nil
}

func (s *service) RuncRestore(ctx context.Context, args *task.RuncRestoreArgs) (*task.RuncRestoreResp, error) {

	opts := &container.RuncOpts{
		Root:          args.Opts.Root,
		Bundle:        args.Opts.Bundle,
		ConsoleSocket: args.Opts.ConsoleSocket,
		Detatch:       args.Opts.Detatch,
	}

	err := s.Client.RuncRestore(args.ImagePath, args.ContainerId, opts)
	if err != nil {
		return nil, err
	}

	return &task.RuncRestoreResp{}, nil
}

func (s *service) publishStateContinous(rate int) {
	s.Client.logger.Info().Msgf("pid: %d", s.Client.Process.PID)
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

	pid, err := s.runTask(args.Task)

	if err == nil {
		s.Client.logger.Info().Msgf("managing process with pid %d", pid)
		s.Client.state.Flag = task.FlagEnum_JOB_RUNNING
		s.Client.Process.PID = pid
	} else {
		// TODO BS: this should be at market level
		s.Client.logger.Info().Msgf("failed to run task with error: %v, attempt %d", err, 1)
		s.Client.state.Flag = task.FlagEnum_JOB_STARTUP_FAILED
		// TODO BS: replace doom loop with just retrying from market
	}
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

	client, err := InstantiateClient()
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

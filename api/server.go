package api

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	task "github.com/cedana/cedana/api/services/task"
	"github.com/cedana/cedana/container"
	"github.com/cedana/cedana/utils"
	"github.com/rs/zerolog"
	"golang.org/x/time/rate"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
)

const defaultLogPath string = "/var/log/cedana-output.log"

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
	logger            *zerolog.Logger
	ClientLogStream   task.TaskService_LogStreamingServer
	ClientStateStream task.TaskService_ClientStateStreamingServer
	r                 *os.File
	w                 *os.File
	task.UnimplementedTaskServiceServer
}

func (s *service) Dump(ctx context.Context, args *task.DumpArgs) (*task.DumpResp, error) {
	s.Client.jobID = args.JobID
	// Close before dumping
	s.r.Close()
	s.w.Close()

	cfg := utils.Config{}
	store := utils.NewCedanaStore(&cfg)

	pid := args.PID

	if args.Type != task.DumpArgs_MARKET {
		s.Client.generateState(args.PID)
		var state task.ProcessState

		state.Flag = task.FlagEnum_JOB_RUNNING
		state.PID = pid

		err := s.Client.db.CreateOrUpdateCedanaProcess(args.JobID, &state)
		if err != nil {
			err = status.Error(codes.NotFound, "db not found")
			return nil, err
		}
	}

	err := s.Client.Dump(args.Dir, args.PID)
	if err != nil {
		err = status.Error(codes.InvalidArgument, "Invalid arguments")
		return nil, err
	}

	var resp task.DumpResp

	switch args.Type {
	case task.DumpArgs_SELF_SERVE:
		// if not market - we don't push up the checkpoint to anywhere
		// we've checkpointed, just return
		resp = task.DumpResp{
			Message: fmt.Sprintf("Dumped process %d to %s", args.PID, args.Dir),
		}

	case task.DumpArgs_MARKET:
		state, err := s.Client.db.GetStateFromID(args.JobID)
		if err != nil {
			err = status.Error(codes.NotFound, "jobid not found in db")
			return nil, err
		}

		if state == nil {
			err = status.Error(codes.NotFound, fmt.Sprintf("state not found for job %v", args.JobID))
			return nil, err
		}

		checkpointPath := state.CheckpointPath

		file, err := os.Open(checkpointPath)
		if err != nil {
			err := status.Error(codes.Unavailable, "StartMultiPartUpload failed")
			return nil, err
		}
		defer file.Close()

		// Get the file size
		fileInfo, err := file.Stat()
		if err != nil {
			err = status.Error(codes.NotFound, "checkpoint zip not found")
			return nil, err
		}

		// Get the size
		size := fileInfo.Size()

		// zipFileSize += 4096

		checkpointFullSize := int64(size)

		multipartCheckpointResp, cid, err := store.CreateMultiPartUpload(checkpointFullSize)
		if err != nil {
			err := status.Error(codes.Unavailable, "CreateMultiPartUpload failed")
			return nil, err
		}

		err = store.StartMultiPartUpload(cid, &multipartCheckpointResp, checkpointPath)
		if err != nil {
			err := status.Error(codes.Unavailable, "StartMultiPartUpload failed")
			return nil, err
		}

		err = store.CompleteMultiPartUpload(multipartCheckpointResp, cid)
		if err != nil {
			err := status.Error(codes.Unavailable, "CompleteMultiPartUpload failed")
			return nil, err
		}

		resp = task.DumpResp{
			Message: fmt.Sprintf("Dumped process %d to %s, multipart checkpoint id: %s", args.PID, args.Dir, multipartCheckpointResp.UploadID),
		}
	}

	return &resp, nil
}

func (s *service) Restore(ctx context.Context, args *task.RestoreArgs) (*task.RestoreResp, error) {
	client := s.Client

	switch args.Type {
	case task.RestoreArgs_LOCAL:
		// assume a suitable file has been passed to args

		pid, err := client.Restore(args)
		if err != nil {
			staterr := status.Error(codes.Internal, fmt.Sprintf("failed to restore process: %v", err))
			return nil, staterr
		}

		return &task.RestoreResp{
			Message: fmt.Sprintf("Successfully restored process: %v", *pid),
			NewPID:  *pid,
		}, err

	case task.RestoreArgs_REMOTE:
		client.remoteStore = utils.NewCedanaStore(client.config)
		zipFile, err := client.remoteStore.GetCheckpoint(args.CheckpointId)
		if err != nil {
			return nil, err
		}

		pid, err := client.Restore(&task.RestoreArgs{
			Type:           task.RestoreArgs_REMOTE,
			CheckpointId:   args.CheckpointId,
			CheckpointPath: *zipFile,
		})

		return &task.RestoreResp{
			Message: fmt.Sprintf("Successfully restored process: %v", *pid),
			NewPID:  *pid,
		}, err
	}
	return &task.RestoreResp{}, nil
}

func (s *service) ContainerDump(ctx context.Context, args *task.ContainerDumpArgs) (*task.ContainerDumpResp, error) {
	err := s.Client.ContainerDump(args.Ref, args.ContainerId)
	if err != nil {
		err = status.Error(codes.InvalidArgument, "arguments are invalid, container not found")
		return nil, err
	}
	return &task.ContainerDumpResp{}, nil
}

func (s *service) ContainerRestore(ctx context.Context, args *task.ContainerRestoreArgs) (*task.ContainerRestoreResp, error) {
	err := s.Client.ContainerRestore(args.ImgPath, args.ContainerId)
	if err != nil {
		err = status.Error(codes.InvalidArgument, "arguments are invalid, container not found")
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
		err = status.Error(codes.InvalidArgument, "invalid argument")
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
		err = status.Error(codes.InvalidArgument, "invalid argument")
		return nil, err
	}

	return &task.RuncRestoreResp{Message: fmt.Sprintf("Restored %v, succesfully", args.ContainerId)}, nil
}

func (s *service) publishStateContinous(rate int) {
	// get PID from id
	pid, err := s.Client.db.GetPID(s.Client.jobID)
	if err != nil {
		s.logger.Warn().Msgf("could not get pid: %v", err)
	}
	s.logger.Info().Msgf("pid: %d", pid)
	ticker := time.NewTicker(time.Duration(rate) * time.Second)
	for range ticker.C {
		if pid != 0 {
			args := &task.ProcessState{}

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
			s.logger.Debug().Msgf("Client has closed connection")
			break
		}
		if err != nil {
			s.logger.Debug().Msgf("Unable to read from client, %v", err)
			return err
		}

		if s.ClientStateStream != nil {

			args := &task.ProcessState{}
			if err := s.ClientStateStream.Send(args); err != nil {
				s.logger.Debug().Msgf("Issue sending process state")
				break
			}
		}
	}
	return nil
}

func (s *service) runTask(task, workingDir, logOutputFile string) (int32, error) {
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

	if workingDir != "" {
		cmd.Dir = workingDir
	}

	cmd.Stdin = nullFile
	if logOutputFile == "" {
		// default to /var/log/cedana-output.log
		logOutputFile = defaultLogPath
	}
	outputFile, err := os.OpenFile(logOutputFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o655)
	if err != nil {
		return 0, err
	}

	cmd.Stdout = io.MultiWriter(w, outputFile)
	cmd.Stderr = io.MultiWriter(w, outputFile)

	cmd.Env = os.Environ()

	err = cmd.Start()
	if err != nil {
		return 0, err
	}

	go func() {
		err := cmd.Wait()
		if err != nil {
			s.logger.Error().Err(err).Msgf("task terminated with: %v", err)
			buf := make([]byte, 4096)
			_, err := r.Read(buf)
			if err != nil {
				s.logger.Warn().Err(err).Msgf("could not read stderr, file likely closed")
			}
		}
	}()

	pid = int32(cmd.Process.Pid)
	ppid := int32(os.Getpid())

	closeCommonFds(ppid, pid)
	return pid, nil
}

func (s *service) StartTask(ctx context.Context, args *task.StartTaskArgs) (*task.StartTaskResp, error) {

	var state task.ProcessState
	var taskToRun string

	if args.Task == "" {
		taskToRun = s.Client.config.Client.Task
	} else {
		taskToRun = args.Task
	}

	pid, err := s.runTask(taskToRun, args.WorkingDir, args.LogOutputFile)

	if err == nil {
		s.Client.logger.Info().Msgf("managing process with pid %d", pid)

		state.Flag = task.FlagEnum_JOB_RUNNING
		state.PID = pid
	} else {
		// TODO BS: this should be at market level
		s.Client.logger.Info().Msgf("failed to run task with error: %v, attempt %d", err, 1)
		state.Flag = task.FlagEnum_JOB_STARTUP_FAILED
		// TODO BS: replace doom loop with just retrying from market
	}
	err = s.Client.db.CreateOrUpdateCedanaProcess(args.Id, &state)

	if err != nil {
		err = status.Error(codes.InvalidArgument, "invalid argument")
		return nil, err
	}

	if state.Flag == task.FlagEnum_JOB_STARTUP_FAILED {
		err = status.Error(codes.Internal, "Task setup failed")
		return nil, err
	}

	return &task.StartTaskResp{
		Message: fmt.Sprintf("Started task: %v", pid),
		PID:     pid,
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

	logger := utils.GetLogger()

	service := &service{
		Client: client,
		logger: &logger,
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

func StartGRPCServer() (*grpc.Server, error) {
	var wg sync.WaitGroup

	// Create a context with a cancel function
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv, err := addGRPC()
	if err != nil {
		return nil, err
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
	// signal.Notify(interrupt, os.Interrupt, syscall.SIGTERM)

	select {
	case <-interrupt:
	case <-ctx.Done():
	}

	wg.Wait()

	// Cleanup here

	return srv.grpcServer, nil
}

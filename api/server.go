package api

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	kube "github.com/cedana/cedana/api/kube"
	"github.com/cedana/cedana/api/runc"
	task "github.com/cedana/cedana/api/services/task"
	"github.com/cedana/cedana/container"
	"github.com/cedana/cedana/utils"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/attribute"
	"golang.org/x/sys/unix"
	"golang.org/x/time/rate"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
)

const defaultLogPath string = "/var/log/cedana-output.log"

const (
	k8sDefaultRuncRoot  = "/run/containerd/runc/k8s.io"
	cedanaContainerName = "binary-container"
)

type GrpcService interface {
	Register(*grpc.Server) error
}

type UploadResponse struct {
	UploadID  string `json:"upload_id"`
	PartSize  int    `json:"part_size"`
	PartCount int    `json:"part_count"`
}

type service struct {
	client            *Client
	logger            *zerolog.Logger
	ClientLogStream   task.TaskService_LogStreamingServer
	ClientStateStream task.TaskService_ClientStateStreamingServer
	r                 *os.File
	w                 *os.File
	task.UnimplementedTaskServiceServer
}

func (s *service) Dump(ctx context.Context, args *task.DumpArgs) (*task.DumpResp, error) {
	var pid int32

	ctx, dumpTracer := s.client.tracer.Start(ctx, "dump-ckpt")
	dumpTracer.SetAttributes(attribute.String("jobID", args.JobID))
	defer dumpTracer.End()
	s.client.jobID = args.JobID

	s.r.Close()
	s.w.Close()

	cfg, err := utils.InitConfig()
	if err != nil {
		err = status.Error(codes.Internal, err.Error())
		return nil, err
	}

	store := utils.NewCedanaStore(cfg, s.client.tracer)

	// job vs process checkpointing, where a PID is provided directly
	if args.PID != 0 {
		pid = args.PID
	} else {
		pid, err = s.client.db.GetPID(args.JobID)
		if err != nil {
			err = status.Error(codes.Internal, err.Error())
			return nil, err
		}
	}

	s.client.generateState(pid)
	var state task.ProcessState

	state.Flag = task.FlagEnum_JOB_RUNNING
	state.PID = pid

	err = s.client.db.CreateOrUpdateCedanaProcess(args.JobID, &state)
	if err != nil {
		err = status.Error(codes.Internal, err.Error())
		return nil, err
	}

	err = s.client.Dump(ctx, args.Dir, pid)
	if err != nil {
		st := status.New(codes.Internal, err.Error())
		dumpTracer.RecordError(st.Err())
		return nil, st.Err()
	}

	var resp task.DumpResp

	switch args.Type {
	case task.DumpArgs_LOCAL:
		resp = task.DumpResp{
			Message: fmt.Sprintf("Dumped process %d to %s", pid, args.Dir),
		}

	case task.DumpArgs_REMOTE:
		state, err := s.client.db.GetStateFromID(args.JobID)
		if err != nil {
			st := status.New(codes.Internal, err.Error())
			return nil, st.Err()
		}

		if state == nil {
			st := status.New(codes.NotFound, fmt.Sprintf("state not found for job %v", args.JobID))
			st.WithDetails(&errdetails.ErrorInfo{
				Reason: err.Error(),
			})
			return nil, st.Err()
		}

		checkpointPath := state.CheckpointPath

		file, err := os.Open(checkpointPath)
		if err != nil {
			st := status.New(codes.NotFound, "checkpoint zip not found")
			st.WithDetails(&errdetails.ErrorInfo{
				Reason: err.Error(),
			})
			return nil, st.Err()
		}
		defer file.Close()

		fileInfo, err := file.Stat()
		if err != nil {
			st := status.New(codes.Internal, "checkpoint zip stat failed")
			st.WithDetails(&errdetails.ErrorInfo{
				Reason: err.Error(),
			})
			return nil, st.Err()
		}

		// Get the size
		size := fileInfo.Size()

		// zipFileSize += 4096
		checkpointFullSize := int64(size)

		ctx, uploadSpan := s.client.tracer.Start(ctx, "upload-ckpt")
		multipartCheckpointResp, cid, err := store.CreateMultiPartUpload(ctx, checkpointFullSize)
		if err != nil {
			st := status.New(codes.Internal, fmt.Sprintf("CreateMultiPartUpload failed with error: %s", err.Error()))
			uploadSpan.RecordError(err)
			return nil, st.Err()
		}

		err = store.StartMultiPartUpload(ctx, cid, multipartCheckpointResp, checkpointPath)
		if err != nil {
			st := status.New(codes.Internal, fmt.Sprintf("StartMultiPartUpload failed with error: %s", err.Error()))
			uploadSpan.RecordError(err)
			return nil, st.Err()
		}

		err = store.CompleteMultiPartUpload(ctx, *multipartCheckpointResp, cid)
		if err != nil {
			st := status.New(codes.Internal, fmt.Sprintf("CompleteMultiPartUpload failed with error: %s", err.Error()))
			uploadSpan.RecordError(err)
			return nil, st.Err()
		}
		uploadSpan.End()

		remoteState := &task.RemoteState{CheckpointID: cid, UploadID: multipartCheckpointResp.UploadID, Timestamp: time.Now().Unix()}

		state.RemoteState = append(state.RemoteState, remoteState)

		s.client.db.UpdateProcessStateWithID(args.JobID, state)

		resp = task.DumpResp{
			Message:      fmt.Sprintf("Dumped process %d to %s, multipart checkpoint id: %s", pid, args.Dir, multipartCheckpointResp.UploadID),
			CheckpointID: cid,
			UploadID:     multipartCheckpointResp.UploadID,
		}
	}

	return &resp, nil
}

func (s *service) Restore(ctx context.Context, args *task.RestoreArgs) (*task.RestoreResp, error) {
	ctx, restoreTracer := s.client.tracer.Start(ctx, "restore-ckpt")
	restoreTracer.SetAttributes(attribute.String("jobID", args.JobID))
	defer restoreTracer.End()
	var resp task.RestoreResp

	switch args.Type {

	case task.RestoreArgs_LOCAL:
		// get checkpointPath from db
		// assume a suitable file has been passed to args
		pid, err := s.client.Restore(ctx, args)
		if err != nil {
			staterr := status.Error(codes.Internal, fmt.Sprintf("failed to restore process: %v", err))
			restoreTracer.RecordError(staterr)
			return nil, staterr
		}

		resp = task.RestoreResp{
			Message: fmt.Sprintf("Successfully restored process: %v", *pid),
			NewPID:  *pid,
		}

	case task.RestoreArgs_REMOTE:
		if args.CheckpointId == "" {
			return nil, status.Error(codes.InvalidArgument, "checkpoint id cannot be empty")
		}

		cfg, err := utils.InitConfig()
		if err != nil {
			err = status.Error(codes.Internal, err.Error())
			return nil, err
		}

		store := utils.NewCedanaStore(cfg, s.client.tracer)

		zipFile, err := store.GetCheckpoint(ctx, args.CheckpointId)
		if err != nil {
			return nil, err
		}

		pid, err := s.client.Restore(ctx, &task.RestoreArgs{
			Type:           task.RestoreArgs_REMOTE,
			CheckpointId:   args.CheckpointId,
			CheckpointPath: *zipFile,
		})

		if err != nil {
			staterr := status.Error(codes.Internal, fmt.Sprintf("failed to restore process: %v", err))
			restoreTracer.RecordError(staterr)
			return nil, staterr
		}

		resp = task.RestoreResp{
			Message: fmt.Sprintf("Successfully restored process: %v", *pid),
			NewPID:  *pid,
		}
	}

	return &resp, nil
}

func (s *service) ContainerDump(ctx context.Context, args *task.ContainerDumpArgs) (*task.ContainerDumpResp, error) {
	err := s.client.ContainerDump(args.Ref, args.ContainerId)
	if err != nil {
		err = status.Error(codes.Internal, err.Error())
		return nil, err
	}
	return &task.ContainerDumpResp{}, nil
}

func (s *service) ContainerRestore(ctx context.Context, args *task.ContainerRestoreArgs) (*task.ContainerRestoreResp, error) {
	err := s.client.ContainerRestore(args.ImgPath, args.ContainerId)
	if err != nil {
		err = status.Error(codes.InvalidArgument, "arguments are invalid, container not found")
		return nil, err
	}
	return &task.ContainerRestoreResp{}, nil
}

func (s *service) RuncDump(ctx context.Context, args *task.RuncDumpArgs) (*task.RuncDumpResp, error) {
	var uploadID string
	var checkpointId string
	//TODO BS: This will be done at controller level, just doing it here for now...
	jobId := uuid.New().String()
	pid, err := runc.GetPidByContainerId(args.ContainerId, args.Root)
	if err != nil {
		err = status.Error(codes.Internal, err.Error())
		return nil, err
	}
	s.client.generateState(int32(pid))
	var state task.ProcessState

	state.Flag = task.FlagEnum_JOB_RUNNING
	state.PID = int32(pid)

	err = s.client.db.CreateOrUpdateCedanaProcess(jobId, &state)
	if err != nil {
		err = status.Error(codes.Internal, err.Error())
		return nil, err
	}

	s.client.jobID = jobId

	criuOpts := &container.CriuOpts{
		ImagesDirectory: args.CriuOpts.ImagesDirectory,
		WorkDirectory:   args.CriuOpts.WorkDirectory,
		LeaveRunning:    true,
		TcpEstablished:  args.CriuOpts.TcpEstablished,
		MntnsCompatMode: true,
	}
	cfg, err := utils.InitConfig()
	if err != nil {
		err = status.Error(codes.Internal, err.Error())
		return nil, err
	}
	store := utils.NewCedanaStore(cfg, s.client.tracer)

	err = s.client.RuncDump(ctx, args.Root, args.ContainerId, criuOpts)
	if err != nil {
		st := status.New(codes.Internal, "Runc dump failed")
		st.WithDetails(&errdetails.ErrorInfo{
			Reason: err.Error(),
		})
		return nil, st.Err()
	}

	if args.Type == task.RuncDumpArgs_REMOTE {
		state, err := s.client.db.GetStateFromID(jobId)
		if err != nil {
			st := status.New(codes.Internal, err.Error())
			return nil, st.Err()
		}

		if state == nil {
			st := status.New(codes.NotFound, fmt.Sprintf("state not found for job %v", jobId))
			st.WithDetails(&errdetails.ErrorInfo{
				Reason: err.Error(),
			})
			return nil, st.Err()
		}

		checkpointPath := state.CheckpointPath

		file, err := os.Open(checkpointPath)
		if err != nil {
			st := status.New(codes.NotFound, "checkpoint zip not found")
			st.WithDetails(&errdetails.ErrorInfo{
				Reason: err.Error(),
			})
			return nil, st.Err()
		}
		defer file.Close()

		fileInfo, err := file.Stat()
		if err != nil {
			st := status.New(codes.Internal, "checkpoint zip stat failed")
			st.WithDetails(&errdetails.ErrorInfo{
				Reason: err.Error(),
			})
			return nil, st.Err()
		}

		// Get the size
		size := fileInfo.Size()

		// zipFileSize += 4096
		checkpointFullSize := int64(size)

		multipartCheckpointResp, cid, err := store.CreateMultiPartUpload(ctx, checkpointFullSize)
		if err != nil {
			st := status.New(codes.Internal, fmt.Sprintf("CreateMultiPartUpload failed with error: %s", err.Error()))
			return nil, st.Err()
		}

		checkpointId = cid

		err = store.StartMultiPartUpload(ctx, cid, multipartCheckpointResp, checkpointPath)
		if err != nil {
			st := status.New(codes.Internal, fmt.Sprintf("StartMultiPartUpload failed with error: %s", err.Error()))
			return nil, st.Err()
		}

		err = store.CompleteMultiPartUpload(ctx, *multipartCheckpointResp, cid)
		if err != nil {
			st := status.New(codes.Internal, fmt.Sprintf("CompleteMultiPartUpload failed with error: %s", err.Error()))
			return nil, st.Err()
		}

		remoteState := &task.RemoteState{CheckpointID: cid, UploadID: multipartCheckpointResp.UploadID, Timestamp: time.Now().Unix()}

		state.RemoteState = append(state.RemoteState, remoteState)

		uploadID = multipartCheckpointResp.UploadID

		s.client.db.UpdateProcessStateWithID(jobId, state)

	}

	return &task.RuncDumpResp{Message: fmt.Sprintf("Dumped process %s to %s, multipart checkpoint id: %s", jobId, args.CriuOpts.ImagesDirectory, uploadID), CheckpointId: checkpointId}, nil
}

func (s *service) RuncRestore(ctx context.Context, args *task.RuncRestoreArgs) (*task.RuncRestoreResp, error) {

	opts := &container.RuncOpts{
		Root:          args.Opts.Root,
		Bundle:        args.Opts.Bundle,
		ConsoleSocket: args.Opts.ConsoleSocket,
		Detatch:       args.Opts.Detatch,
		NetPid:        int(args.Opts.NetPid),
	}
	switch args.Type {
	case task.RuncRestoreArgs_LOCAL:
		err := s.client.RuncRestore(ctx, args.ImagePath, args.ContainerId, args.IsK3S, []string{}, opts)
		if err != nil {
			err = status.Error(codes.InvalidArgument, "invalid argument")
			return nil, err
		}

	case task.RuncRestoreArgs_REMOTE:
		if args.CheckpointId == "" {
			return nil, status.Error(codes.InvalidArgument, "checkpoint id cannot be empty")
		}

		cfg, err := utils.InitConfig()
		if err != nil {
			err = status.Error(codes.Internal, err.Error())
			return nil, err
		}

		store := utils.NewCedanaStore(cfg, s.client.tracer)

		zipFile, err := store.GetCheckpoint(ctx, args.CheckpointId)
		if err != nil {
			return nil, err
		}

		err = s.client.RuncRestore(ctx, *zipFile, args.ContainerId, args.IsK3S, []string{}, opts)

		if err != nil {
			staterr := status.Error(codes.Internal, fmt.Sprintf("failed to restore process: %v", err))
			return nil, staterr
		}

	}

	return &task.RuncRestoreResp{Message: fmt.Sprintf("Restored %v, succesfully", args.ContainerId)}, nil
}

func (s *service) ListContainers(ctx context.Context, args *task.ListArgs) (*task.ListResp, error) {
	var containers []*task.Container

	annotations, err := kube.StateList(args.Root)
	if err != nil {
		return nil, err
	}

	for _, sandbox := range annotations {
		var container task.Container

		if sandbox[kube.CONTAINER_TYPE] == kube.CONTAINER_TYPE_CONTAINER {
			container.ContainerName = sandbox[kube.CONTAINER_NAME]
			container.ImageName = sandbox[kube.IMAGE_NAME]
			container.SandboxId = sandbox[kube.SANDBOX_ID]
			container.SandboxName = sandbox[kube.SANDBOX_NAME]
			container.SandboxUid = sandbox[kube.SANDBOX_UID]
			container.SandboxNamespace = sandbox[kube.SANDBOX_NAMESPACE]

			if sandbox[kube.SANDBOX_NAMESPACE] == args.Namespace || args.Namespace == "" && container.ImageName != "" {
				containers = append(containers, &container)
			}
		}
	}

	return &task.ListResp{
		Containers: containers,
	}, nil
}

func (s *service) GetRuncContainerByName(ctx context.Context, args *task.CtrByNameArgs) (*task.CtrByNameResp, error) {
	runcId, bundle, err := runc.GetContainerIdByName(args.ContainerName, args.Root)
	if err != nil {
		return nil, err
	}
	resp := &task.CtrByNameResp{
		RuncContainerName: runcId,
		RuncBundlePath:    bundle,
	}
	return resp, nil
}

func (s *service) GetPausePid(ctx context.Context, args *task.PausePidArgs) (*task.PausePidResp, error) {
	pid, err := runc.GetPausePid(args.BundlePath)
	if err != nil {
		return nil, err
	}
	resp := &task.PausePidResp{
		PausePid: int64(pid),
	}
	return resp, nil
}

func (s *service) publishStateContinous(rate int) {
	// get PID from id
	pid, err := s.client.db.GetPID(s.client.jobID)
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

func (s *service) runTask(ctx context.Context, task string, args *task.StartTaskArgs) (int32, error) {
	ctx, span := s.client.tracer.Start(ctx, "exec")
	span.SetAttributes(attribute.String("task", task))
	defer span.End()

	var pid int32
	if task == "" {
		return 0, fmt.Errorf("could not find task in config")
	}

	var gpuCmd *exec.Cmd
	var err error
	if os.Getenv("CEDANA_GPU_ENABLED") == "true" {
		_, gpuStartSpan := s.client.tracer.Start(ctx, "start-gpu-controller")
		gpuCmd, err = StartGPUController(args.UID, args.GID, s.logger)
		if err != nil {
			return 0, err
		}
		gpuStartSpan.End()
	}

	nullFile, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		return 0, err
	}

	cmd := exec.Command("bash", "-c", task)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
		Credential: &syscall.Credential{
			Uid: args.UID,
			Gid: args.GID,
		},
	}

	// working dir needs to be consistent on the checkpoint and restore side
	if args.WorkingDir != "" {
		cmd.Dir = args.WorkingDir
	}

	cmd.Stdin = nullFile
	if args.LogOutputFile == "" {
		args.LogOutputFile = defaultLogPath
	}

	// is this non-performant? do we need to flush at intervals instead of writing?
	outputFile, err := os.OpenFile(args.LogOutputFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o777)
	if err != nil {
		return 0, err
	}

	var stderrbuf bytes.Buffer
	var gpuerrbuf bytes.Buffer

	cmd.Stdout = outputFile
	cmd.Stderr = io.MultiWriter(outputFile, &stderrbuf)

	if gpuCmd != nil {
		gpuCmd.Stderr = io.Writer(&gpuerrbuf)
	}

	cmd.Env = args.Env
	err = cmd.Start()
	if err != nil {
		return 0, err
	}

	go func() {
		err := cmd.Wait()
		if gpuCmd != nil {
			err = gpuCmd.Process.Kill()
			if err != nil {
				s.logger.Fatal().Err(err)
			}
			s.logger.Info().Msgf("GPU controller killed with pid: %d", gpuCmd.Process.Pid)
			// read last bit of data from /tmp/cedana-gpucontroller.log and print
			s.logger.Info().Msgf("GPU controller log: %v", gpuerrbuf.String())
		}
		if err != nil {
			s.logger.Warn().Msgf("task terminated with error: %v", stderrbuf.String())
			s.logger.Error().Err(err).Msgf("task terminated with: %v", err)
		}
	}()

	pid = int32(cmd.Process.Pid)
	ppid := int32(os.Getpid())

	closeCommonFds(ppid, pid)
	return pid, nil
}

func StartGPUController(uid, gid uint32, logger *zerolog.Logger) (*exec.Cmd, error) {
	logger.Debug().Msgf("starting gpu controller with uid: %d, gid: %d", uid, gid)
	var gpuCmd *exec.Cmd
	controllerPath := os.Getenv("GPU_CONTROLLER_PATH")
	if controllerPath == "" {
		err := fmt.Errorf("gpu controller path not set")
		logger.Fatal().Err(err)
		return nil, err
	}

	if os.Getenv("CEDANA_GPU_DEBUGGING_ENABLED") == "true" {
		controllerPath = strings.Join([]string{
			"compute-sanitizer",
			"--log-file /tmp/cedana-sanitizer.log",
			"--print-level info",
			"--leak-check=full",
			controllerPath},
			" ")
		// wrap controller path in a string
		logger.Info().Msgf("GPU controller started with args: %v", controllerPath)
	}

	gpuCmd = exec.Command("bash", "-c", controllerPath)
	gpuCmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
		Credential: &syscall.Credential{
			Uid: uid,
			Gid: gid,
		},
	}

	gpuCmd.Stderr = nil
	gpuCmd.Stdout = nil

	err := gpuCmd.Start()
	go func() {
		err := gpuCmd.Wait()
		if err != nil {
			logger.Fatal().Err(err)
		}
	}()
	if err != nil {
		logger.Fatal().Err(err)
	}
	logger.Info().Msgf("GPU controller started with pid: %d, logging to: /tmp/cedana-gpucontroller.log", gpuCmd.Process.Pid)
	return gpuCmd, nil
}

func (s *service) StartTask(ctx context.Context, args *task.StartTaskArgs) (*task.StartTaskResp, error) {
	var state task.ProcessState
	var taskToRun string

	if args.Task == "" {
		taskToRun = s.client.config.Client.Task
	} else {
		taskToRun = args.Task
	}

	pid, err := s.runTask(ctx, taskToRun, args)

	if err == nil {
		s.client.logger.Info().Msgf("managing process with pid %d", pid)

		state.Flag = task.FlagEnum_JOB_RUNNING
		state.PID = pid
	} else {
		// TODO BS: this should be at market level
		s.client.logger.Info().Msgf("failed to run task with error: %v, attempt %d", err, 1)
		state.Flag = task.FlagEnum_JOB_STARTUP_FAILED
		// TODO BS: replace doom loop with just retrying from market
	}
	err = s.client.db.CreateOrUpdateCedanaProcess(args.Id, &state)

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
		client: client,
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
		// Here join netns
		//TODO find pause bundle path
		if os.Getenv("IS_K8S") == "1" {
			_, bundle, err := runc.GetContainerIdByName(cedanaContainerName, k8sDefaultRuncRoot)
			if err != nil {
				fmt.Println(err.Error())
				os.Exit(1)
			}

			pausePid, err := runc.GetPausePid(bundle)
			if err != nil {
				fmt.Println(err.Error())
				os.Exit(1)
			}

			nsFd, err := unix.Open(fmt.Sprintf("/proc/%s/ns/net", strconv.Itoa(pausePid)), unix.O_RDONLY, 0)
			if err != nil {
				fmt.Println("Error opening network namespace:", err)
				os.Exit(1)
			}
			defer unix.Close(nsFd)

			// Join the network namespace of the target process
			err = unix.Setns(nsFd, unix.CLONE_NEWNET)
			if err != nil {
				fmt.Println("Error setting network namespace:", err)
				os.Exit(1)
			}
		}

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

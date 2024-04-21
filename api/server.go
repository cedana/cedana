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
	"syscall"
	"time"

	kube "github.com/cedana/cedana/api/kube"
	"github.com/cedana/cedana/api/runc"
	task "github.com/cedana/cedana/api/services/task"
	"github.com/cedana/cedana/container"
	"github.com/cedana/cedana/utils"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/spf13/viper"
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
	Address  = "localhost:8080"
	Protocol = "tcp"

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
	ClientLogStream   task.TaskService_LogStreamingServer
	ClientStateStream task.TaskService_ClientStateStreamingServer
	r                 *os.File
	w                 *os.File
	task.UnimplementedTaskServiceServer
}

func (s *service) Dump(ctx context.Context, args *task.DumpArgs) (*task.DumpResp, error) {
	var pid int32
	var err error

	ctx, dumpTracer := s.client.tracer.Start(ctx, "dump-ckpt")
	dumpTracer.SetAttributes(attribute.String("jobID", args.JobID))
	defer dumpTracer.End()
	s.client.jobID = args.JobID

	s.r.Close()
	s.w.Close()

	store := utils.NewCedanaStore(s.client.tracer)

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

		store := utils.NewCedanaStore(s.client.tracer)

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
	// TODO BS: This will be done at controller level, just doing it here for now...
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
	store := utils.NewCedanaStore(s.client.tracer)

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

		store := utils.NewCedanaStore(s.client.tracer)

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
		logger.Warn().Msgf("could not get pid: %v", err)
	}
	logger.Info().Msgf("pid: %d", pid)
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
			logger.Debug().Msgf("Client has closed connection")
			break
		}
		if err != nil {
			logger.Debug().Msgf("Unable to read from client, %v", err)
			return err
		}

		if s.ClientStateStream != nil {

			args := &task.ProcessState{}
			if err := s.ClientStateStream.Send(args); err != nil {
				logger.Debug().Msgf("Issue sending process state")
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
	if viper.GetBool("gpu_enabled") {
		_, gpuStartSpan := s.client.tracer.Start(ctx, "start-gpu-controller")
		gpuCmd, err = StartGPUController(args.UID, args.GID, logger)
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
				logger.Fatal().Err(err)
			}
			logger.Info().Msgf("GPU controller killed with pid: %d", gpuCmd.Process.Pid)
			// read last bit of data from /tmp/cedana-gpucontroller.log and print
			logger.Info().Msgf("GPU controller log: %v", gpuerrbuf.String())
		}
		if err != nil {
			logger.Warn().Msgf("task terminated with error: %v", stderrbuf.String())
			logger.Error().Err(err).Msgf("task terminated with: %v", err)
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

	if viper.GetBool("gpu_debugging_enabled") {
		controllerPath = strings.Join([]string{
			"compute-sanitizer",
			"--log-file /tmp/cedana-sanitizer.log",
			"--print-level info",
			"--leak-check=full",
			controllerPath,
		},
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
		logger.Info().Msgf("managing process with pid %d", pid)

		state.Flag = task.FlagEnum_JOB_RUNNING
		state.PID = pid
	} else {
		// TODO BS: this should be at market level
		logger.Info().Msgf("failed to run task with error: %v, attempt %d", err, 1)
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
	listener   net.Listener
}

func NewServer() (*Server, error) {
	server := &Server{
		grpcServer: grpc.NewServer(),
	}
	client, err := InstantiateClient()
	if err != nil {
		return nil, err
	}
	service := &service{
		client: client,
	}
	task.RegisterTaskServiceServer(server.grpcServer, service)
	reflection.Register(server.grpcServer)

	listener, err := net.Listen(Protocol, Address)
	if err != nil {
		return nil, err
	}
	server.listener = listener

	return server, err
}

func (s *Server) start() error {
	return s.grpcServer.Serve(s.listener)
}

func (s *Server) stop() error {
	s.grpcServer.GracefulStop()
	return s.listener.Close()
}

// Takes in a context that allows for cancellation from the cmdline
func StartServer(cmdCtx context.Context) error {
	// Create a child context for the server
	srvCtx, cancel := context.WithCancelCause(cmdCtx)
	defer cancel(nil)

	server, err := NewServer()
	if err != nil {
		return err
	}

	go func() {
		// Here join netns
		// TODO find pause bundle path
		if viper.GetBool("is_k8s") {
			_, bundle, err := runc.GetContainerIdByName(cedanaContainerName, k8sDefaultRuncRoot)
			if err != nil {
				cancel(err)
				return
			}

			pausePid, err := runc.GetPausePid(bundle)
			if err != nil {
				cancel(err)
				return
			}

			nsFd, err := unix.Open(fmt.Sprintf("/proc/%s/ns/net", strconv.Itoa(pausePid)), unix.O_RDONLY, 0)
			if err != nil {
				cancel(fmt.Errorf("Error opening network namespace: %v", err))
				return
			}
			defer unix.Close(nsFd)

			// Join the network namespace of the target process
			err = unix.Setns(nsFd, unix.CLONE_NEWNET)
			if err != nil {
				cancel(fmt.Errorf("Error setting network namespace: %v", err))
			}
		}

		logger.Debug().Msgf("started RPC server at %s", Address)
		err := server.start()
		if err != nil {
			cancel(err)
		}
	}()

	select {
	case <-srvCtx.Done():
		err = srvCtx.Err()
	case <-cmdCtx.Done():
		err = cmdCtx.Err()
		server.stop()
	}

	logger.Debug().Msg("stopped RPC server gracefully")

	return err
}

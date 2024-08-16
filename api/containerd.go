package api

// Implements the task service functions for containerd

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/cedana/cedana/api/containerd"
	"github.com/cedana/cedana/api/kube"
	"github.com/cedana/cedana/api/runc"
	"github.com/cedana/cedana/api/services/task"
	"github.com/cedana/cedana/container"
	"github.com/cedana/cedana/utils"
	"github.com/containerd/containerd/namespaces"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func readDumpLog(imagesDir string) (string, error) {
	logPath := filepath.Join(imagesDir, "dump.log")
	file, err := os.ReadFile(logPath)
	if err != nil {
		return "", fmt.Errorf("failed to read dump.log: %w", err)
	}
	return string(file), nil
}

func (s *service) ContainerdDump(ctx context.Context, args *task.ContainerdDumpArgs) (*task.ContainerdDumpResp, error) {
	rootfsOpts := args.ContainerdRootfsDumpArgs
	dumpOpts := args.RuncDumpArgs

	ctx = namespaces.WithNamespace(ctx, rootfsOpts.Namespace)

	containerdService, err := containerd.New(ctx, rootfsOpts.Address, s.logger)
	if err != nil {
		return nil, err
	}

	runcContainer := container.GetContainerFromRunc(dumpOpts.ContainerID, dumpOpts.Root)

	tcpPath := fmt.Sprintf("/proc/%v/net/tcp", runcContainer.Pid)
	getReader := func() (io.Reader, error) {
		file, err := os.Open(tcpPath)
		if err != nil {
			return nil, err
		}
		return file, nil
	}

	fdDir := fmt.Sprintf("/proc/%d/fd/", runcContainer.Pid)

	isReady, err := utils.IsReadyLoop(utils.GetTCPStates, getReader, utils.IsUsingIoUring, 30, 100, fdDir)
	if err != nil {
		return nil, err
	}

	containerdTask, err := containerdService.CgroupFreeze(ctx, rootfsOpts.ContainerID)
	if err != nil {
		return nil, err
	}
	if containerdTask != nil {
		defer func() {
			if err := containerdTask.Resume(ctx); err != nil {
				fmt.Println(fmt.Errorf("error resuming task: %w", err))
			}
		}()
	}

	isReady, err = utils.IsReadyLoop(utils.GetTCPStates, getReader, utils.IsUsingIoUring, 1, 0, fdDir)
	if err != nil {
		return nil, err
	}

	_, err = containerdService.DumpRootfs(ctx, rootfsOpts.ContainerID, rootfsOpts.ImageRef, rootfsOpts.Namespace)
	if err != nil {
		return nil, err
	}

	dumpStats := task.DumpStats{
		DumpType: task.DumpType_RUNC,
	}
	ctx = context.WithValue(ctx, "dumpStats", &dumpStats)

	// RUNC DUMP ARGS START
	pid, err := runc.GetPidByContainerId(dumpOpts.ContainerID, dumpOpts.Root)
	if err != nil {
		err = status.Error(codes.Internal, fmt.Sprintf("failed to get pid by container id: %v", err))
		return nil, err
	}

	state, err := s.generateState(ctx, pid)
	if err != nil {
		err = status.Error(codes.Internal, err.Error())
		return nil, err
	}
	state.JobState = task.JobState_JOB_RUNNING

	isUsingTCP, err := CheckTCPConnections(pid)
	if err != nil {
		return nil, err
	}

	criuOpts := &container.CriuOpts{
		ImagesDirectory: dumpOpts.CriuOpts.ImagesDirectory,
		WorkDirectory:   dumpOpts.CriuOpts.WorkDirectory,
		LeaveRunning:    true,
		TcpEstablished:  isUsingTCP,
		TcpClose:        isUsingTCP,
		MntnsCompatMode: false,
		External:        dumpOpts.CriuOpts.External,
		TCPInFlight:     !isReady,
	}

	err = s.runcDump(ctx, dumpOpts.Root, dumpOpts.ContainerID, dumpOpts.Pid, criuOpts, state)
	if err != nil {
		dumpLogContent, logErr := readDumpLog(dumpOpts.CriuOpts.ImagesDirectory)
		if logErr != nil {
			dumpLogContent = "Failed to read dump.log: " + logErr.Error()
		}

		st := status.New(codes.Internal, "Runc dump failed")
		st.WithDetails(&errdetails.ErrorInfo{
			Reason: err.Error() + "\nDump.log content:\n" + dumpLogContent,
		})
		return nil, st.Err()
	}

	var resp task.RuncDumpResp

	switch dumpOpts.Type {
	case task.CRType_LOCAL:
		resp = task.RuncDumpResp{
			Message: fmt.Sprintf("Dumped runc process %d", pid),
		}

	case task.CRType_REMOTE:
		checkpointID, uploadID, err := s.uploadCheckpoint(ctx, state.CheckpointPath)
		if err != nil {
			st := status.New(codes.Internal, fmt.Sprintf("failed to upload checkpoint with error: %s", err.Error()))
			return nil, st.Err()
		}
		remoteState := &task.RemoteState{CheckpointID: checkpointID, UploadID: uploadID, Timestamp: time.Now().Unix()}
		state.RemoteState = append(state.RemoteState, remoteState)
		resp = task.RuncDumpResp{
			Message:      fmt.Sprintf("Dumped runc process %d, multipart checkpoint id: %s", pid, uploadID),
			CheckpointID: checkpointID,
			UploadID:     uploadID,
		}
	}

	resp.State = state

	return &task.ContainerdDumpResp{
		Message:        "Dumped containerd container",
		CheckpointPath: state.CheckpointPath,
	}, nil
}

func (s *service) ContainerdRestore(ctx context.Context, args *task.ContainerdRestoreArgs) (*task.ContainerdRestoreResp, error) {
	err := s.containerdRestore(ctx, args.ImgPath, args.ContainerID)
	if err != nil {
		err = status.Error(codes.InvalidArgument, "arguments are invalid, container not found")
		return nil, err
	}
	return &task.ContainerdRestoreResp{
		Message: "Restored containerd container",
	}, nil
}

func (s *service) ContainerdQuery(ctx context.Context, args *task.ContainerdQueryArgs) (*task.ContainerdQueryResp, error) {
	var containers []*task.ContainerdContainer

	runcContainers, err := kube.StateList(args.Root)
	if err != nil {
		return nil, err
	}

	for _, c := range runcContainers {
		var container task.ContainerdContainer

		if c.Annotations[kube.CONTAINER_TYPE] == kube.CONTAINER_TYPE_CONTAINER {
			container.ContainerName = c.Annotations[kube.CONTAINER_NAME]
			container.ImageName = c.Annotations[kube.IMAGE_NAME]
			container.SandboxId = c.Annotations[kube.SANDBOX_ID]
			container.SandboxName = c.Annotations[kube.SANDBOX_NAME]
			container.SandboxUid = c.Annotations[kube.SANDBOX_UID]
			container.SandboxNamespace = c.Annotations[kube.SANDBOX_NAMESPACE]

			if c.Annotations[kube.SANDBOX_NAMESPACE] == args.Namespace || args.Namespace == "" && container.ImageName != "" {
				containers = append(containers, &container)
			}
		}
	}

	return &task.ContainerdQueryResp{
		Containers: containers,
	}, nil
}

func (s *service) ContainerdRootfsDump(ctx context.Context, args *task.ContainerdRootfsDumpArgs) (*task.ContainerdRootfsDumpResp, error) {

	containerdService, err := containerd.New(ctx, args.Address, s.logger)
	if err != nil {
		return &task.ContainerdRootfsDumpResp{}, err
	}

	ref, err := containerdService.DumpRootfs(ctx, args.ContainerID, args.ImageRef, args.Namespace)
	if err != nil {
		return &task.ContainerdRootfsDumpResp{}, err
	}

	return &task.ContainerdRootfsDumpResp{ImageRef: ref}, nil
}

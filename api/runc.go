package api

// Implements the task service functions for runc

import (
	"context"
	"fmt"
	"time"

	"github.com/cedana/cedana/api/runc"
	"github.com/cedana/cedana/api/services/task"
	container "github.com/cedana/cedana/container"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *service) RuncDump(ctx context.Context, args *task.RuncDumpArgs) (*task.RuncDumpResp, error) {
	pid, err := runc.GetPidByContainerId(args.ContainerID, args.Root)
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

	criuOpts := &container.CriuOpts{
		ImagesDirectory: args.CriuOpts.ImagesDirectory,
		WorkDirectory:   args.CriuOpts.WorkDirectory,
		LeaveRunning:    true,
		TcpEstablished:  args.CriuOpts.TcpEstablished,
		MntnsCompatMode: false,
		External:        args.CriuOpts.External,
	}

	err = s.runcDump(ctx, args.Root, args.ContainerID, args.Pid, criuOpts, state)
	if err != nil {
		st := status.New(codes.Internal, "Runc dump failed")
		st.WithDetails(&errdetails.ErrorInfo{
			Reason: err.Error(),
		})
		return nil, st.Err()
	}

	var resp task.RuncDumpResp

	switch args.Type {
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

	return &resp, err
}

func (s *service) RuncRestore(ctx context.Context, args *task.RuncRestoreArgs) (*task.RuncRestoreResp, error) {
	opts := &container.RuncOpts{
		Root:          args.Opts.Root,
		Bundle:        args.Opts.Bundle,
		ConsoleSocket: args.Opts.ConsoleSocket,
		Detach:        args.Opts.Detach,
		NetPid:        int(args.Opts.NetPid),
		StateRoot:     args.Opts.Root,
	}
	switch args.Type {
	case task.CRType_LOCAL:
		err := s.runcRestore(ctx, args.ImagePath, args.ContainerID, args.IsK3S, []string{}, opts)
		if err != nil {
			err = status.Error(codes.Internal, err.Error())
			return nil, err
		}

	case task.CRType_REMOTE:
		if args.CheckpointID == "" {
			return nil, status.Error(codes.InvalidArgument, "checkpoint id cannot be empty")
		}
		zipFile, err := s.store.GetCheckpoint(ctx, args.CheckpointID)
		if err != nil {
			return nil, err
		}
		err = s.runcRestore(ctx, *zipFile, args.ContainerID, args.IsK3S, []string{}, opts)
		if err != nil {
			staterr := status.Error(codes.Internal, fmt.Sprintf("failed to restore process: %v", err))
			return nil, staterr
		}
	}

	pid, err := runc.GetPidByContainerId(args.ContainerID, args.Opts.Root)
	if err != nil {
		return nil, status.Error(codes.NotFound, fmt.Sprintf("failed to get pid by container id: %v", err))
	}

	state, err := s.generateState(ctx, pid)
	if err != nil {
		return nil, status.Error(codes.Internal, fmt.Sprintf("failed to generate state: %v", err))
	}

	// TODO: Update state to add or use a job that exists for this container
	return &task.RuncRestoreResp{
		Message: fmt.Sprintf("Restored %v, succesfully", args.ContainerID),
		State:   state,
	}, nil
}

func (s *service) RuncQuery(ctx context.Context, args *task.RuncQueryArgs) (*task.RuncQueryResp, error) {
	var containers []*task.RuncContainer
	if len(args.ContainerNames) == 0 {

		runcContainers, err := runc.RuncGetAll(args.Root, args.Namespace)
		if err != nil {
			return nil, status.Error(codes.NotFound, fmt.Sprintf("Container \"%s\" not found"))
		}

		for _, c := range runcContainers {
			ctr := &task.RuncContainer{
				ID:            c.ContainerId,
				ImageName:     c.ImageName,
				BundlePath:    c.Bundle,
				ContainerName: c.ContainerName,
				SandboxId:     c.SandboxId,
				SandboxName:   c.SandboxName,
				SandboxUid:    c.SandboxUid,
			}
			containers = append(containers, ctr)
		}
	}
	for i, name := range args.ContainerNames {
		runcId, bundle, err := runc.GetContainerIdByName(name, args.SandboxNames[i], args.Root)
		if err != nil {
			return nil, status.Error(codes.NotFound, fmt.Sprintf("Container \"%s\" not found", name))
		}
		containers = append(containers, &task.RuncContainer{
			ID:         runcId,
			BundlePath: bundle,
		})
	}

	return &task.RuncQueryResp{Containers: containers}, nil
}

func (s *service) RuncGetPausePid(ctx context.Context, args *task.RuncGetPausePidArgs) (*task.RuncGetPausePidResp, error) {
	pid, err := runc.GetPausePid(args.BundlePath)
	if err != nil {
		return nil, status.Error(codes.NotFound, fmt.Sprintf("pause pid not found: %v", err))
	}
	resp := &task.RuncGetPausePidResp{
		PausePid: int64(pid),
	}
	return resp, nil
}

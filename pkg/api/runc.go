package api

// Implements the task service functions for runc

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cedana/cedana/pkg/api/runc"
	"github.com/cedana/cedana/pkg/api/services/task"
	container "github.com/cedana/cedana/pkg/container"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/spf13/viper"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// checks if the given process has any active tcp connections
func CheckTCPConnections(pid int32) (bool, error) {
	tcpFile := filepath.Join("/proc", fmt.Sprintf("%d", pid), "net/tcp")

	file, err := os.Open(tcpFile)
	if err != nil {
		return false, fmt.Errorf("failed to open %s: %v", tcpFile, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "  sl") {
			continue
		}
		return true, nil
	}

	if err := scanner.Err(); err != nil {
		return false, fmt.Errorf("error reading %s: %v", tcpFile, err)
	}

	return false, nil
}

func (s *service) RuncDump(ctx context.Context, args *task.RuncDumpArgs) (*task.RuncDumpResp, error) {
	dumpStats := task.DumpStats{
		DumpType: task.DumpType_RUNC,
	}
	ctx = context.WithValue(ctx, utils.DumpStatsKey, &dumpStats)

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

	isUsingTCP, err := CheckTCPConnections(pid)
	if err != nil {
		return nil, err
	}

	criuOpts := &container.CriuOpts{
		ImagesDirectory: args.GetCriuOpts().GetImagesDirectory(),
		WorkDirectory:   args.GetCriuOpts().GetWorkDirectory(),
		LeaveRunning:    args.GetCriuOpts().GetLeaveRunning() || viper.GetBool("client.leave_running"),
		TcpEstablished:  isUsingTCP || args.GetCriuOpts().GetTcpEstablished(),
		TcpClose:        isUsingTCP,
		MntnsCompatMode: false,
		External:        args.GetCriuOpts().GetExternal(),
		FileLocks:       args.GetCriuOpts().GetFileLocks(),
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
	resp.DumpStats = &dumpStats

	return &resp, err
}

func (s *service) RuncRestore(ctx context.Context, args *task.RuncRestoreArgs) (*task.RuncRestoreResp, error) {
	restoreStats := task.RestoreStats{
		DumpType: task.DumpType_RUNC,
	}
	ctx = context.WithValue(ctx, "restoreStats", &restoreStats)

	opts := &container.RuncOpts{
		Root:          args.GetOpts().GetRoot(),
		Bundle:        args.GetOpts().GetBundle(),
		ConsoleSocket: args.GetOpts().GetConsoleSocket(),
		Detach:        args.GetOpts().GetDetach(),
		NetPid:        int(args.GetOpts().GetNetPid()),
		StateRoot:     args.GetOpts().GetRoot(),
	}

	criuOpts := &container.CriuOpts{
		MntnsCompatMode: false, // XXX: Should instead take value from args
		TcpClose:        true,  // XXX: Should instead take value from args
		TcpEstablished:  args.GetCriuOpts().GetTcpEstablished(),
		FileLocks:       args.GetCriuOpts().GetFileLocks(),
	}

	if viper.GetBool("remote") {
		args.Type = task.CRType_REMOTE
	} else {
		args.Type = task.CRType_LOCAL
	}

	switch args.Type {
	case task.CRType_LOCAL:
		err := s.runcRestore(ctx, args.ImagePath, args.ContainerID, criuOpts, opts)
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
		err = s.runcRestore(ctx, *zipFile, args.ContainerID, criuOpts, opts)
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
		Message:      fmt.Sprintf("Restored %v, successfully", args.ContainerID),
		State:        state,
		RestoreStats: &restoreStats,
	}, nil
}

func (s *service) RuncQuery(ctx context.Context, args *task.RuncQueryArgs) (*task.RuncQueryResp, error) {
	var containers []*task.RuncContainer
	if len(args.ContainerNames) == 0 {

		runcContainers, err := runc.RuncGetAll(args.Root, args.Namespace)
		if err != nil {
			return nil, status.Error(codes.NotFound, fmt.Sprint("Container not found"))
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

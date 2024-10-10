package api

// Implements the task service functions for runc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"syscall"
	"time"

	"github.com/cedana/cedana/pkg/api/runc"
	"github.com/cedana/cedana/pkg/api/services/task"
	container "github.com/cedana/cedana/pkg/container"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/rs/zerolog/log"
	"github.com/spf13/viper"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *service) RuncManage(ctx context.Context, args *task.RuncManageArgs) (*task.RuncManageResp, error) {
	// For runc we reuse the runc container ID as JID

	// Check if job ID is already in use
	state, err := s.getState(ctx, args.ContainerID)
	if state != nil {
		err = status.Error(codes.AlreadyExists, "job ID already exists")
		return nil, err
	}

	// get pid
	pid, err := runc.GetPidByContainerId(args.ContainerID, args.Root)

	// Check if process PID already exists as a managed job
	queryResp, err := s.JobQuery(ctx, &task.JobQueryArgs{PIDs: []int32{pid}})
	if len(queryResp.Processes) > 0 {
		if queryResp.Processes[0].JobState == task.JobState_JOB_RUNNING {
			err = status.Error(codes.AlreadyExists, "PID already exists as a managed job")
			return nil, err
		}
	}

	state, err = s.generateState(ctx, pid)
	if state == nil || state.ProcessInfo.IsRunning == false {
		err = status.Error(codes.NotFound, "process not running")
		return nil, err
	}
	state.JID = args.ContainerID
	state.ContainerID = args.ContainerID
	state.ContainerRoot = args.Root
	state.JobState = task.JobState_JOB_RUNNING

	var gpuCmd *exec.Cmd
	gpuOutBuf := &bytes.Buffer{}
	if args.GPU {
		log.Info().Msg("GPU support requested, assuming process was already started with LD_PRELOAD")
		if args.GPU {
			gpuOut := io.Writer(gpuOutBuf)
			gpuCmd, err = s.StartGPUController(ctx, args.UID, args.GID, args.Groups, gpuOut)
			if err != nil {
				log.Error().Err(err).Str("stdout/stderr", gpuOutBuf.String()).Msg("failed to start GPU controller")
				return nil, fmt.Errorf("failed to start GPU controller: %v", err)
			}
		}
		state.GPU = true
	}

	// Wait for server shutdown to gracefully exit, since job is now managed
	// Wait for process exit, to update state, and clean up GPU controller
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		select {
		case <-s.serverCtx.Done():
			<-s.serverCtx.Done()
			log.Info().Str("JID", state.JID).Int32("PID", state.PID).Msg("server shutting down, killing process")
			err := syscall.Kill(int(state.PID), syscall.SIGKILL)
			if err != nil {
				log.Error().Err(err).Str("JID", state.JID).Int32("PID", state.PID).Msg("failed to kill process")
			}
		case <-utils.WaitForPid(state.PID):
			log.Info().Str("JID", state.JID).Int32("PID", state.PID).Msg("process exited")
		}
		state.JobState = task.JobState_JOB_DONE
		err := s.updateState(context.WithoutCancel(ctx), state.JID, state)
		if err != nil {
			log.Error().Err(err).Msg("failed to update state after done")
		}
		if gpuCmd != nil {
			err = gpuCmd.Process.Kill()
			if err != nil {
				log.Error().Err(err).Msg("failed to kill GPU controller after process exit")
			}
		}
	}()

	// Clean up GPU controller and also handle premature exit
	if gpuCmd != nil {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			err := gpuCmd.Wait()
			if err != nil {
				log.Debug().Err(err).Msg("GPU controller Wait()")
			}
			log.Info().Int("PID", gpuCmd.Process.Pid).
				Int("status", gpuCmd.ProcessState.ExitCode()).
				Str("out/err", gpuOutBuf.String()).
				Msg("GPU controller exited")

			// Should kill process if still running since GPU controller might have exited prematurely
			syscall.Kill(int(state.PID), syscall.SIGKILL)
		}()
	}

	err = s.updateState(ctx, state.JID, state)
	if err != nil {
		err = status.Error(codes.Internal, "failed to update state")
		return nil, err
	}

	return nil, nil
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

	isUsingTCP, err := utils.CheckTCPConnections(pid)
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

	err = s.runcDump(ctx, args.Root, args.ContainerID, pid, criuOpts, state)
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
	ctx = context.WithValue(ctx, utils.RestoreStatsKey, &restoreStats)

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
			return nil, status.Error(codes.NotFound, err.Error())
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
		if errors.Is(err, runc.ErrContainerNotFound) {
			log.Info().Msgf("container %s not found", name)
		}

		if !errors.Is(err, runc.ErrContainerNotFound) && err != nil {
			return nil, err
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

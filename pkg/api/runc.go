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

	pid, err := runc.GetPidByContainerId(args.ContainerID, args.Root)
	if err != nil {
		return nil, status.Error(codes.NotFound, fmt.Sprintf("failed to get pid by container id: %v", err))
	}
	bundle, err := runc.GetBundleByContainerId(args.ContainerID, args.Root)
	if err != nil {
		return nil, status.Error(codes.NotFound, fmt.Sprintf("failed to get bundle by container id: %v", err))
	}

	// Check if process PID already exists as a managed job
	queryResp, err := s.JobQuery(ctx, &task.JobQueryArgs{PIDs: []int32{pid}})
	if len(queryResp.Processes) > 0 {
		if queryResp.Processes[0].JobState == task.JobState_JOB_RUNNING {
			err = status.Error(codes.AlreadyExists, "PID already exists as a managed job")
			return nil, err
		}
	}

	exitCode := utils.WaitForPid(pid)

	state, err = s.generateState(ctx, pid)
	if state == nil || state.ProcessInfo.IsRunning == false {
		err = status.Error(codes.NotFound, "process not running")
		return nil, err
	}
	state.JID = args.ContainerID
	state.ContainerID = args.ContainerID
	state.ContainerRoot = args.Root
	state.ContainerBundle = bundle
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

	err = s.updateState(ctx, state.JID, state)
	if err != nil {
		log.Error().Err(err).Msg("failed to update state after manage")
		syscall.Kill(int(pid), syscall.SIGKILL) // kill cuz inconsistent state
		return nil, status.Error(codes.Internal, "failed to update state after manage")
	}

	// Wait for server shutdown to gracefully exit, since job is now managed
	// Also wait for process exit, to update state
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		select {
		case <-s.serverCtx.Done():
			<-s.serverCtx.Done()
			log.Info().Str("JID", state.JID).Int32("PID", state.PID).Msg("server shutting down, killing runc container")
			err := syscall.Kill(int(state.PID), syscall.SIGKILL)
			if err != nil {
				log.Error().Err(err).Str("JID", state.JID).Int32("PID", state.PID).Msg("failed to kill runc container")
			}
		case <-exitCode:
			log.Info().Str("JID", state.JID).Int32("PID", state.PID).Msg("runc container exited")
		}
		state, err = s.getState(context.WithoutCancel(ctx), state.JID)
		if err != nil {
			log.Warn().Err(err).Msg("failed to get latest state, DB might be inconsistent")
		}
		state.JobState = task.JobState_JOB_DONE
		err := s.updateState(context.WithoutCancel(ctx), state.JID, state)
		if err != nil {
			log.Error().Err(err).Msg("failed to update state after done")
		}
		if gpuCmd != nil {
			err = gpuCmd.Process.Kill()
			if err != nil {
				log.Error().Err(err).Msg("failed to kill GPU controller after runc container exit")
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

	return &task.RuncManageResp{Message: "success", State: state}, nil
}

func (s *service) RuncDump(ctx context.Context, args *task.RuncDumpArgs) (*task.RuncDumpResp, error) {
	dumpStats := task.DumpStats{
		DumpType: task.DumpType_RUNC,
	}
	ctx = context.WithValue(ctx, utils.DumpStatsKey, &dumpStats)

	pid, err := runc.GetPidByContainerId(args.ContainerID, args.Root)
	if err != nil {
		return nil, status.Error(codes.NotFound, fmt.Sprintf("failed to get pid by container id: %v", err))
	}
	bundle, err := runc.GetBundleByContainerId(args.ContainerID, args.Root)
	if err != nil {
		return nil, status.Error(codes.NotFound, fmt.Sprintf("failed to get bundle by container id: %v", err))
	}

	if args.Dir == "" {
		args.Dir = viper.GetString("shared_storage.dump_storage_dir")
		if args.Dir == "" {
			return nil, status.Error(codes.InvalidArgument, "dump storage dir not provided/found in config")
		}
	}

	isManagedJob := false // if a JID = ContainerID exists in DB
	state, err := s.getState(ctx, args.ContainerID)
	if err == nil {
		isManagedJob = true
		if state.GPU && s.gpuEnabled == false {
			return nil, status.Error(codes.FailedPrecondition, "GPU support is not enabled in daemon")
		}
	}

	state, err = s.generateState(ctx, pid)
	if err != nil {
		err = status.Error(codes.Internal, err.Error())
		return nil, err
	}
	state.ContainerID = args.ContainerID
	state.ContainerRoot = args.Root
	state.ContainerBundle = bundle

	isUsingTCP, err := utils.CheckTCPConnections(pid)
	if err != nil {
		return nil, err
	}

	criuOpts := &container.CriuOpts{
		ImagesDirectory: args.Dir,
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
		st := status.New(codes.Internal, "Runc dump failed: "+err.Error())
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

	// Only update state if it was a managed job
	if isManagedJob {
		err = s.updateState(ctx, state.JID, state)
		if err != nil {
			st := status.New(codes.Internal, fmt.Sprintf("failed to update state with error: %s", err.Error()))
			return nil, st.Err()
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

	isManagedJob := false // if a JID = ContainerID exists in DB
	state, err := s.getState(ctx, args.ContainerID)
	if err == nil {
		isManagedJob = true
		if state.GPU && s.gpuEnabled == false {
			return nil, status.Error(codes.FailedPrecondition, "Dump has GPU state and GPU support is not enabled in daemon")
		}
	}

	if isManagedJob {
		if viper.GetBool("remote") {
			remoteState := state.GetRemoteState()
			if remoteState == nil {
				log.Debug().Str("JID", args.ContainerID).Msgf("No remote state found")
				return nil, status.Error(codes.InvalidArgument, "no remote state found")
			}
			// For now just grab latest checkpoint
			if remoteState[len(remoteState)-1].CheckpointID == "" {
				log.Debug().Str("JID", args.ContainerID).Msgf("No remote checkpoint found")
				return nil, status.Error(codes.InvalidArgument, "no remote checkpoint found")
			}
			args.CheckpointID = remoteState[len(remoteState)-1].CheckpointID
			args.Type = task.CRType_REMOTE
		} else {
			args.ImagePath = state.CheckpointPath[:len(state.CheckpointPath)-4]
			args.Type = task.CRType_LOCAL
		}
	} else {
		args.Type = task.CRType_LOCAL
	}

	switch args.Type {
	case task.CRType_LOCAL:
		if args.ImagePath == "" {
			return nil, status.Error(codes.InvalidArgument, "checkpoint path cannot be empty")
		}

	case task.CRType_REMOTE:
		if args.CheckpointID == "" {
			return nil, status.Error(codes.InvalidArgument, "checkpoint id cannot be empty")
		}

		zipFile, err := s.store.GetCheckpoint(ctx, args.CheckpointID)
		if err != nil {
			return nil, err
		}

		args.ImagePath = *zipFile
	}

	pid, exitCode, err := s.runcRestore(ctx, args.ImagePath, args.ContainerID, criuOpts, opts, isManagedJob)
	if err != nil {
		err = status.Error(codes.Internal, fmt.Sprintf("failed to restore runc container: %v", err))
		return nil, err
	}

	// Only update state if it was a managed job
	if isManagedJob {
		state, err = s.getState(ctx, args.ContainerID)
		if err != nil {
			log.Warn().Err(err).Msg("failed to get latest state, DB might be inconsistent")
		}
		log.Info().Int32("PID", pid).Str("JID", state.JID).Msgf("managing restored runc container")
		state.PID = pid
		state.JobState = task.JobState_JOB_RUNNING
		err = s.updateState(ctx, state.JID, state)
		if err != nil {
			log.Error().Err(err).Msg("failed to update state after restore")
			syscall.Kill(int(pid), syscall.SIGKILL) // kill cuz inconsistent state
			return nil, status.Error(codes.Internal, "failed to update state after restore")
		}

		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			<-exitCode
			state, err = s.getState(context.WithoutCancel(ctx), state.JID)
			if err != nil {
				log.Warn().Err(err).Msg("failed to get latest state, DB might be inconsistent")
			}
			state.JobState = task.JobState_JOB_DONE
			err = s.updateState(context.WithoutCancel(ctx), state.JID, state)
			if err != nil {
				log.Error().Err(err).Msg("failed to update state after done")
				return
			}
		}()
	}

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

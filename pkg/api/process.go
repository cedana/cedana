package api

// Implements the task service functions for processes

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/cedana/cedana/pkg/api/services/task"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/rs/xid"
	"github.com/shirou/gopsutil/v3/process"
	"github.com/spf13/viper"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/rs/zerolog/log"
)

const (
	OUTPUT_FILE_PATH  string      = "/var/log/cedana-output.log"
	OUTPUT_FILE_PERMS os.FileMode = 0o777
	OUTPUT_FILE_FLAGS int         = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
)

var DB_BUCKET_JOBS = []byte("jobs")

func (s *service) Start(ctx context.Context, args *task.StartArgs) (*task.StartResp, error) {
	return s.startHelper(ctx, args, nil)
}

func (s *service) StartAttach(stream task.TaskService_StartAttachServer) error {
	in, err := stream.Recv()
	if err != nil {
		return err
	}
	args := in.GetArgs()

	_, err = s.startHelper(stream.Context(), args, stream)
	return err
}

func (s *service) Manage(ctx context.Context, args *task.ManageArgs) (*task.ManageResp, error) {
	if args.JID == "" {
		args.JID = xid.New().String()
	} else {
		// Check if job ID is already in use
		state, err := s.getState(ctx, args.JID)
		if state != nil {
			err = status.Error(codes.AlreadyExists, "job ID already exists")
			return nil, err
		}
	}

	// Check if process PID already exists as a managed job
	queryResp, err := s.JobQuery(ctx, &task.JobQueryArgs{PIDs: []int32{args.PID}})
	if len(queryResp.Processes) > 0 {
		if queryResp.Processes[0].JobState == task.JobState_JOB_RUNNING {
			err = status.Error(codes.AlreadyExists, "PID already exists as a managed job")
			return nil, err
		}
	}

	state, err := s.generateState(ctx, args.PID)
	if state == nil || state.ProcessInfo.IsRunning == false {
		err = status.Error(codes.NotFound, "process not running")
		return nil, err
	}
	state.JID = args.JID
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

			sharedLibPath := viper.GetString("gpu_shared_lib_path")
			if sharedLibPath == "" {
				sharedLibPath = utils.GpuSharedLibPath
			}
			if _, err := os.Stat(sharedLibPath); os.IsNotExist(err) {
				return nil, fmt.Errorf("no gpu shared lib at %s", sharedLibPath)
			}
		}
		state.GPU = true
	}

	err = s.updateState(ctx, state.JID, state)
	if err != nil {
		err = status.Error(codes.Internal, "failed to update state")
		return nil, err
	}

	// Wait for server shutdown to gracefully exit, since job is now managed
	// Also wait for process exit, to update state
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

	return &task.ManageResp{Message: "success", State: state}, nil
}

func (s *service) Dump(ctx context.Context, args *task.DumpArgs) (*task.DumpResp, error) {
	var err error

	dumpStats := task.DumpStats{
		DumpType: task.DumpType_PROCESS,
	}
	ctx = context.WithValue(ctx, utils.DumpStatsKey, &dumpStats)

	if args.Dir == "" {
		args.Dir = viper.GetString("shared_storage.dump_storage_dir")
		if args.Dir == "" {
			return nil, status.Error(codes.InvalidArgument, "dump storage dir not provided/found in config")
		}
	}

	if viper.GetBool("remote") {
		args.Type = task.CRType_REMOTE
	} else {
		args.Type = task.CRType_LOCAL
	}

	state := &task.ProcessState{}
	pid := args.PID

	if args.JID != "" { // if job
		state, err = s.getState(ctx, args.JID)
		if err != nil {
			err = status.Error(codes.NotFound, err.Error())
			return nil, err
		}
		if state.GPU && s.gpuEnabled == false {
			return nil, status.Error(codes.FailedPrecondition, "GPU support is not enabled in daemon")
		}
		pid = state.PID
	}

	state, err = s.generateState(ctx, pid)
	if err != nil {
		err = status.Error(codes.Internal, err.Error())
		return nil, err
	}

	err = s.dump(ctx, state, args)
	if err != nil {
		st := status.New(codes.Internal, err.Error())
		return nil, st.Err()
	}

	var resp task.DumpResp

	switch args.Type {
	case task.CRType_LOCAL:
		resp = task.DumpResp{
			Message:      fmt.Sprintf("Dumped process %d to %s", pid, args.Dir),
			CheckpointID: state.CheckpointPath, // XXX: Just return path for ID for now
		}

	case task.CRType_REMOTE:
		checkpointID, uploadID, err := s.uploadCheckpoint(ctx, state.CheckpointPath)
		if err != nil {
			st := status.New(codes.Internal, fmt.Sprintf("failed to upload checkpoint with error: %s", err.Error()))
			return nil, st.Err()
		}
		remoteState := &task.RemoteState{CheckpointID: checkpointID, UploadID: uploadID, Timestamp: time.Now().Unix()}
		state.RemoteState = append(state.RemoteState, remoteState)
		resp = task.DumpResp{
			Message:      fmt.Sprintf("Dumped process %d to %s, multipart checkpoint id: %s", pid, args.Dir, uploadID),
			CheckpointID: checkpointID,
			UploadID:     uploadID,
		}
	}

	// Only update state if it was a managed job
	if args.JID != "" {
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

func (s *service) Restore(ctx context.Context, args *task.RestoreArgs) (*task.RestoreResp, error) {
	return s.restoreHelper(ctx, args, nil)
}

func (s *service) RestoreAttach(stream task.TaskService_RestoreAttachServer) error {
	in, err := stream.Recv()
	if err != nil {
		return err
	}
	args := in.GetArgs()

	_, err = s.restoreHelper(stream.Context(), args, stream)
	return err
}

//////////////////////////
///// Process Utils //////
//////////////////////////

func (s *service) startHelper(ctx context.Context, args *task.StartArgs, stream task.TaskService_StartAttachServer) (*task.StartResp, error) {
	if args.Task == "" {
		args.Task = viper.GetString("client.task")
	}

	state := &task.ProcessState{}

	if args.GPU {
		if s.gpuEnabled == false {
			return nil, status.Error(codes.FailedPrecondition, "GPU support is not enabled in daemon")
		}
		state.GPU = true
	}

	if args.JID == "" {
		state.JID = xid.New().String()
	} else {
		existingState, _ := s.getState(ctx, args.JID)
		if existingState != nil {
			return nil, status.Error(codes.AlreadyExists, "job ID already exists")
		}
		state.JID = args.JID
	}
	args.JID = state.JID

	pid, exitCode, err := s.run(ctx, args, stream)
	if err != nil {
		log.Error().Err(err).Msg("failed to run task")
		return nil, status.Error(codes.Internal, "failed to run task")
	}
	log.Info().Int32("PID", pid).Str("JID", state.JID).Msgf("managing process")
	state.PID = pid
	state.JobState = task.JobState_JOB_RUNNING
	err = s.updateState(ctx, state.JID, state)
	if err != nil {
		log.Error().Err(err).Msg("failed to update state after run")
		syscall.Kill(int(pid), syscall.SIGKILL) // kill cuz inconsistent state
		return nil, status.Error(codes.Internal, "failed to update state after run")
	}

	if stream != nil && exitCode != nil {
		code := <-exitCode // if streaming, wait for process to finish
		if stream != nil {
			stream.Send(&task.StartAttachResp{
				ExitCode: int32(code),
			})
		}
		state.JobState = task.JobState_JOB_DONE
		err = s.updateState(context.WithoutCancel(ctx), state.JID, state)
		if err != nil {
			log.Error().Err(err).Msg("failed to update state after done")
			return nil, status.Error(codes.Internal, "failed to update state after done")
		}
	} else {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			<-exitCode
			state.JobState = task.JobState_JOB_DONE
			err = s.updateState(context.WithoutCancel(ctx), state.JID, state)
			if err != nil {
				log.Error().Err(err).Msg("failed to update state after done")
				return
			}
		}()
	}

	return &task.StartResp{
		Message: fmt.Sprint("Job started successfully"),
		PID:     pid,
		JID:     state.JID,
	}, err
}

func (s *service) restoreHelper(ctx context.Context, args *task.RestoreArgs, stream task.TaskService_RestoreAttachServer) (*task.RestoreResp, error) {
	var resp task.RestoreResp
	var pid int32
	var err error

	restoreStats := task.RestoreStats{
		DumpType: task.DumpType_PROCESS,
	}
	ctx = context.WithValue(ctx, utils.RestoreStatsKey, &restoreStats)

	if args.JID != "" {
		state, err := s.getState(ctx, args.JID)
		if err != nil {
			err = status.Error(codes.NotFound, err.Error())
			return nil, err
		}
		if state.GPU && s.gpuEnabled == false {
			return nil, status.Error(codes.FailedPrecondition, "Dump has GPU state and GPU support is not enabled in daemon")
		}
		if viper.GetBool("remote") {
			remoteState := state.GetRemoteState()
			if remoteState == nil {
				log.Debug().Str("JID", args.JID).Msgf("No remote state found")
				return nil, status.Error(codes.InvalidArgument, "no remote state found")
			}
			// For now just grab latest checkpoint
			if remoteState[len(remoteState)-1].CheckpointID == "" {
				log.Debug().Str("JID", args.JID).Msgf("No remote checkpoint found")
				return nil, status.Error(codes.InvalidArgument, "no remote checkpoint found")
			}
			args.CheckpointID = remoteState[len(remoteState)-1].CheckpointID
			args.Type = task.CRType_REMOTE
		} else {
			args.CheckpointPath = state.GetCheckpointPath()
			args.Type = task.CRType_LOCAL
		}
	} else {
		args.Type = task.CRType_LOCAL
	}

	switch args.Type {
	case task.CRType_LOCAL:
		if args.CheckpointPath == "" {
			return nil, status.Error(codes.InvalidArgument, "checkpoint path cannot be empty")
		}
		stat, err := os.Stat(args.CheckpointPath)
		if os.IsNotExist(err) {
			return nil, status.Error(codes.InvalidArgument, "invalid checkpoint path: does not exist")
		}
		if !args.Stream && (stat.IsDir() || !strings.HasSuffix(args.CheckpointPath, ".tar")) {
			return nil, status.Error(codes.InvalidArgument, "invalid checkpoint path: must be tar file")
		}
		if args.Stream && (stat.IsDir() || !strings.HasSuffix(args.CheckpointPath, ".lz4")) {
			return nil, status.Error(codes.InvalidArgument, "invalid checkpoint path: must be lz4 file (--stream enabled)")
		}
	case task.CRType_REMOTE:
		if args.CheckpointID == "" {
			return nil, status.Error(codes.InvalidArgument, "checkpoint id cannot be empty")
		}

		zipFile, err := s.store.GetCheckpoint(ctx, args.CheckpointID)
		if err != nil {
			return nil, err
		}

		args.CheckpointPath = *zipFile
	}

	pid, exitCode, err := s.restore(ctx, args, stream)
	if err != nil {
		staterr := status.Error(codes.Internal, fmt.Sprintf("failed to restore process: %v", err))
		return nil, staterr
	}
	state := &task.ProcessState{}
	// Only update state if it was a managed job
	if args.JID != "" {
		state, err = s.generateState(ctx, pid)
		if err != nil {
			log.Warn().Err(err).Msg("failed to generate state after restore, using fallback")
			state, err = s.getState(ctx, args.JID)
			if err != nil {
				err = status.Error(codes.NotFound, err.Error())
				return nil, err
			}
		}
		log.Info().Int32("PID", pid).Str("JID", state.JID).Msgf("managing restored process")
		state.PID = pid
		state.JobState = task.JobState_JOB_RUNNING
		err = s.updateState(ctx, state.JID, state)
		if err != nil {
			log.Error().Err(err).Msg("failed to update state after restore")
			syscall.Kill(int(pid), syscall.SIGKILL) // kill cuz inconsistent state
			return nil, status.Error(codes.Internal, "failed to update state after restore")
		}

		if stream != nil && exitCode != nil {
			code := <-exitCode // if streaming, wait for process to finish
			if stream != nil {
				stream.Send(&task.RestoreAttachResp{
					ExitCode: int32(code),
				})
			}
			state.JobState = task.JobState_JOB_DONE
			err = s.updateState(context.WithoutCancel(ctx), state.JID, state)
			if err != nil {
				log.Error().Err(err).Msg("failed to update state after done")
				return nil, status.Error(codes.Internal, "failed to update state after done")
			}
		} else {
			s.wg.Add(1)
			go func() {
				defer s.wg.Done()
				<-exitCode
				state.JobState = task.JobState_JOB_DONE
				err = s.updateState(context.WithoutCancel(ctx), state.JID, state)
				if err != nil {
					log.Error().Err(err).Msg("failed to update state after done")
					return
				}
			}()
		}
	}

	resp = task.RestoreResp{
		Message:      fmt.Sprintf("successfully restored process: %v", pid),
		State:        state,
		RestoreStats: &restoreStats,
	}

	return &resp, nil
}

func (s *service) run(ctx context.Context, args *task.StartArgs, stream task.TaskService_StartAttachServer) (int32, chan int, error) {
	var pid int32
	if args.Task == "" {
		return 0, nil, fmt.Errorf("could not find task")
	}

	var gpuCmd *exec.Cmd
	gpuOutBuf := &bytes.Buffer{}
	var err error
	if args.GPU {
		gpuOut := io.Writer(gpuOutBuf)
		gpuCmd, err = s.StartGPUController(ctx, args.UID, args.GID, args.Groups, gpuOut)
		if err != nil {
			log.Error().Err(err).Str("stdout/stderr", gpuOutBuf.String()).Msg("failed to start GPU controller")
			return 0, nil, fmt.Errorf("failed to start GPU controller: %v", err)
		}

		sharedLibPath := viper.GetString("gpu_shared_lib_path")
		if sharedLibPath == "" {
			sharedLibPath = utils.GpuSharedLibPath
		}
		if _, err := os.Stat(sharedLibPath); os.IsNotExist(err) {
			return 0, nil, fmt.Errorf("no gpu shared lib at %s", sharedLibPath)
		}
		args.Task = fmt.Sprintf("LD_PRELOAD=%s %s", sharedLibPath, args.Task)
	}

	groupsUint32 := make([]uint32, len(args.Groups))
	for i, v := range args.Groups {
		groupsUint32[i] = uint32(v)
	}
	var cmdCtx context.Context
	if stream != nil {
		cmdCtx = utils.CombineContexts(s.serverCtx, stream.Context()) // either should terminate the process
	} else {
		cmdCtx = s.serverCtx
	}
	cmd := exec.CommandContext(cmdCtx, "bash", "-c", args.Task)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
		Credential: &syscall.Credential{
			Uid:    uint32(args.UID),
			Gid:    uint32(args.GID),
			Groups: groupsUint32,
		},
	}

	// working dir needs to be consistent on the checkpoint and restore side
	if args.WorkingDir != "" {
		cmd.Dir = args.WorkingDir
	}

	if stream == nil {
		if args.LogOutputFile == "" {
			args.LogOutputFile = OUTPUT_FILE_PATH
		}
		nullFile, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
		defer nullFile.Close()
		if err != nil {
			return 0, nil, err
		}
		cmd.Stdin = nullFile
		outFile, err := os.OpenFile(args.LogOutputFile, OUTPUT_FILE_FLAGS, OUTPUT_FILE_PERMS)
		defer outFile.Close()
		os.Chmod(args.LogOutputFile, OUTPUT_FILE_PERMS)
		if err != nil {
			return 0, nil, err
		}
		cmd.Stdout = outFile
		cmd.Stderr = outFile
	} else {
		stdinPipe, err := cmd.StdinPipe()
		if err != nil {
			return 0, nil, err
		}
		// Receive stdin from stream
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			defer stdinPipe.Close()
			for {
				in, err := stream.Recv()
				if err != nil {
					log.Debug().Err(err).Msg("finished reading stdin")
					return
				}
				_, err = stdinPipe.Write([]byte(in.Stdin))
				if err != nil {
					log.Error().Err(err).Msg("failed to write to stdin")
					return
				}
			}
		}()
		// Scan stdout
		stdoutPipe, err := cmd.StdoutPipe()
		if err != nil {
			return 0, nil, err
		}
		stdoutScanner := bufio.NewScanner(stdoutPipe)
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			defer stdoutPipe.Close()
			for stdoutScanner.Scan() {
				if err := stream.Send(&task.StartAttachResp{Stdout: stdoutScanner.Text() + "\n"}); err != nil {
					log.Error().Err(err).Msg("failed to send stdout")
					return
				}
			}
			if err := stdoutScanner.Err(); err != nil {
				log.Debug().Err(err).Msgf("finished reading stdout")
			}
		}()

		// Scan stdout
		stderrPipe, err := cmd.StderrPipe()
		if err != nil {
			return 0, nil, err
		}
		stderrScanner := bufio.NewScanner(stderrPipe)
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			defer stderrPipe.Close()
			for stderrScanner.Scan() {
				if err := stream.Send(&task.StartAttachResp{Stderr: stderrScanner.Text() + "\n"}); err != nil {
					log.Error().Err(err).Msg("failed to send stderr")
					return
				}
			}
			if err := stderrScanner.Err(); err != nil {
				log.Debug().Err(err).Msgf("finished reading stderr")
			}
		}()
	}

	cmd.Env = args.Env
	err = cmd.Start()
	if err != nil {
		return 0, nil, err
	}

	ppid := int32(os.Getpid())
	pid = int32(cmd.Process.Pid)
	closeCommonFds(ppid, pid)

	s.wg.Add(1)
	exitCode := make(chan int)
	go func() {
		defer s.wg.Done()
		err := cmd.Wait()
		if err != nil {
			log.Debug().Err(err).Msg("process Wait()")
		}
		if gpuCmd != nil {
			err = gpuCmd.Process.Kill()
			if err != nil {
				log.Error().Err(err).Msg("failed to kill GPU controller after process exit")
			}
		}
		log.Info().Int("status", cmd.ProcessState.ExitCode()).Int32("PID", pid).Msg("process exited")
		code := cmd.ProcessState.ExitCode()
		exitCode <- code
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
			cmd.Process.Kill()
		}()
	}

	return pid, exitCode, err
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

func (s *service) uploadCheckpoint(ctx context.Context, path string) (string, string, error) {
	start := time.Now()
	stats, ok := ctx.Value(utils.DumpStatsKey).(*task.DumpStats)
	if !ok {
		return "", "", status.Error(codes.Internal, "failed to get dump stats")
	}

	file, err := os.Open(path)
	if err != nil {
		st := status.New(codes.NotFound, "checkpoint zip not found")
		st.WithDetails(&errdetails.ErrorInfo{
			Reason: err.Error(),
		})
		return "", "", st.Err()
	}
	defer file.Close()

	fileInfo, err := file.Stat()
	if err != nil {
		st := status.New(codes.Internal, "checkpoint zip stat failed")
		st.WithDetails(&errdetails.ErrorInfo{
			Reason: err.Error(),
		})
		return "", "", st.Err()
	}

	// Get the size
	size := fileInfo.Size()

	// zipFileSize += 4096
	checkpointFullSize := int64(size)

	multipartCheckpointResp, cid, err := s.store.CreateMultiPartUpload(ctx, checkpointFullSize)
	if err != nil {
		st := status.New(codes.Internal, fmt.Sprintf("CreateMultiPartUpload failed with error: %s", err.Error()))
		return "", "", st.Err()
	}

	err = s.store.StartMultiPartUpload(ctx, cid, multipartCheckpointResp, path)
	if err != nil {
		st := status.New(codes.Internal, fmt.Sprintf("StartMultiPartUpload failed with error: %s", err.Error()))
		return "", "", st.Err()
	}

	err = s.store.CompleteMultiPartUpload(ctx, *multipartCheckpointResp, cid)
	if err != nil {
		st := status.New(codes.Internal, fmt.Sprintf("CompleteMultiPartUpload failed with error: %s", err.Error()))
		return "", "", st.Err()
	}

	stats.UploadDuration = time.Since(start).Milliseconds()
	return cid, multipartCheckpointResp.UploadID, err
}

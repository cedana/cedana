package api

// Implements the task service functions for processes

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/cedana/cedana/api/services/task"
	"github.com/rs/xid"
	"github.com/shirou/gopsutil/v3/process"
	"github.com/spf13/viper"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	OUTPUT_FILE_PATH  string      = "/var/log/cedana-output.log"
	OUTPUT_FILE_PERMS os.FileMode = 0o777
	OUTPUT_FILE_FLAGS int         = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
)

var DB_BUCKET_JOBS = []byte("jobs")

func (s *service) Start(ctx context.Context, args *task.StartArgs) (*task.StartResp, error) {
	if args.Task == "" {
		args.Task = viper.GetString("client.task")
	}

	state := &task.ProcessState{}

	state.JobState = task.JobState_JOB_RUNNING
	if args.JID == "" {
		state.JID = xid.New().String()
		args.JID = state.JID
	} else {
		existingState, _ := s.getState(ctx, args.JID)
		if existingState != nil {
			return nil, status.Error(codes.AlreadyExists, "job ID already exists")
		}
		state.JID = args.JID
	}

	pid, err := s.run(ctx, args)
	state.PID = pid
	if err != nil {
		// TODO BS: this should be at market level
		s.logger.Error().Err(err).Msgf("failed to run task, attempt %d", 1)
		return nil, status.Error(codes.Internal, "failed to run task")
		// TODO BS: replace doom loop with just retrying from market
	}

	s.logger.Info().Msgf("managing process with pid %d", pid)

	err = s.updateState(ctx, state.JID, state)
	if err != nil {
		s.logger.Error().Err(err).Msg("failed to update state")
		return nil, status.Error(codes.Internal, "failed to update state")
	}

	if state.JobState == task.JobState_JOB_STARTUP_FAILED {
		err = status.Error(codes.Internal, "Task startup failed")
		return nil, err
	}

	return &task.StartResp{
		Message: fmt.Sprint("Job started successfully"),
		PID:     pid,
		JID:     state.JID,
	}, err
}

func (s *service) Dump(ctx context.Context, args *task.DumpArgs) (*task.DumpResp, error) {
	var err error

	ctx, dumpTracer := s.tracer.Start(ctx, "dump-ckpt")
	dumpTracer.SetAttributes(attribute.String("jobID", args.JID))
	defer dumpTracer.End()

	state := &task.ProcessState{}
	pid := args.PID

	if pid == 0 { // if job
		state, err = s.getState(ctx, args.JID)
		if err != nil {
			err = status.Error(codes.NotFound, err.Error())
			return nil, err
		}
		if state == nil {
			return nil, status.Error(codes.NotFound, "job not found")
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
		dumpTracer.RecordError(st.Err())
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
			dumpTracer.RecordError(st.Err())
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

	err = s.updateState(ctx, state.JID, state)
	if err != nil {
		st := status.New(codes.Internal, fmt.Sprintf("failed to update state with error: %s", err.Error()))
		return nil, st.Err()
	}

	return &resp, err
}

func (s *service) Restore(ctx context.Context, args *task.RestoreArgs) (*task.RestoreResp, error) {
	ctx, restoreTracer := s.tracer.Start(ctx, "restore-ckpt")
	restoreTracer.SetAttributes(attribute.String("jobID", args.JID))
	defer restoreTracer.End()

	var resp task.RestoreResp
	var pid *int32
	var err error

	switch args.Type {
	case task.CRType_LOCAL:
		if args.CheckpointPath == "" {
			return nil, status.Error(codes.InvalidArgument, "checkpoint path cannot be empty")
		}
		if stat, err := os.Stat(args.CheckpointPath); os.IsNotExist(err) || stat.IsDir() || !strings.HasSuffix(args.CheckpointPath, ".tar") {
			return nil, status.Error(codes.InvalidArgument, "invalid checkpoint path")
		}

		pid, err = s.restore(ctx, args)
		if err != nil {
			staterr := status.Error(codes.Internal, fmt.Sprintf("failed to restore process: %v", err))
			restoreTracer.RecordError(staterr)
			return nil, staterr
		}

		resp = task.RestoreResp{
			Message: fmt.Sprintf("successfully restored process: %v", *pid),
			NewPID:  *pid,
		}

	case task.CRType_REMOTE:
		if args.CheckpointID == "" {
			return nil, status.Error(codes.InvalidArgument, "checkpoint id cannot be empty")
		}

		zipFile, err := s.store.GetCheckpoint(ctx, args.CheckpointID)
		if err != nil {
			return nil, err
		}

		pid, err := s.restore(ctx, &task.RestoreArgs{
			Type:           task.CRType_REMOTE,
			CheckpointID:   args.CheckpointID,
			CheckpointPath: *zipFile,
		})
		if err != nil {
			staterr := status.Error(codes.Internal, fmt.Sprintf("failed to restore process: %v", err))
			restoreTracer.RecordError(staterr)
			return nil, staterr
		}

		resp = task.RestoreResp{
			Message: fmt.Sprintf("successfully restored process: %v", *pid),
			NewPID:  *pid,
		}
	}

	// We could be restoring on a new machine, so we update the state
	state, err := s.generateState(ctx, *pid)
	if err != nil {
		s.logger.Warn().Err(err).Msg("failed to generate state after restore")
	}
	state.JobState = task.JobState_JOB_RUNNING
	err = s.updateState(ctx, state.JID, state)
	if err != nil {
		s.logger.Warn().Err(err).Msg("failed to update state after restore")
	}

	return &resp, nil
}

func (s *service) Query(ctx context.Context, args *task.QueryArgs) (*task.QueryResp, error) {
	res := &task.QueryResp{}

	if len(args.JIDs) > 0 {
		for _, jid := range args.JIDs {
			state, err := s.getState(ctx, jid)
			if err != nil {
				return nil, status.Error(codes.NotFound, "job not found")
			}
			if state != nil {
				res.Processes = append(res.Processes, state)
			}
		}
	} else {
		pidSet := make(map[int32]bool)
		for _, pid := range args.PIDs {
			pidSet[pid] = true
		}

		list, err := s.db.ListJobs(ctx)
		if err != nil {
			return nil, status.Error(codes.Internal, "failed to retrieve jobs from database")
		}
		for _, job := range list {
			state := task.ProcessState{}
			err = json.Unmarshal(job.State, &state)
			if err != nil {
				return nil, status.Error(codes.Internal, "failed to unmarshal state")
			}
			if len(pidSet) > 0 && !pidSet[state.PID] {
				continue
			}
			res.Processes = append(res.Processes, &state)
		}
	}

	return res, nil
}

//////////////////////////
///// Process Utils //////
//////////////////////////

func (s *service) run(ctx context.Context, args *task.StartArgs) (int32, error) {
	ctx, span := s.tracer.Start(ctx, "exec")
	span.SetAttributes(attribute.String("task", args.Task))
	defer span.End()

	var pid int32
	if args.Task == "" {
		return 0, fmt.Errorf("could not find task")
	}

	var gpuCmd *exec.Cmd
	var err error
	if args.GPU {
		_, gpuStartSpan := s.tracer.Start(ctx, "start-gpu-controller")
		gpuCmd, err = StartGPUController(args.UID, args.GID, s.logger)
		if err != nil {
			return 0, err
		}
		// XXX: force sleep for a few ms to allow GPU controller to start
		time.Sleep(100 * time.Millisecond)
		gpuStartSpan.End()
	}

	nullFile, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		return 0, err
	}

	cmd := exec.Command("bash", "-c", args.Task)
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
		args.LogOutputFile = OUTPUT_FILE_PATH
	}

	// XXX: is this non-performant? do we need to flush at intervals instead of writing?
	outputFile, err := os.OpenFile(args.LogOutputFile, OUTPUT_FILE_FLAGS, OUTPUT_FILE_PERMS)
	if err != nil {
		return 0, err
	}
	os.Chmod(args.LogOutputFile, OUTPUT_FILE_PERMS)

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return 0, err
	}

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return 0, err
	}

	var gpuerrbuf bytes.Buffer
	if gpuCmd != nil {
		gpuCmd.Stderr = io.Writer(&gpuerrbuf)
	}

	cmd.Env = args.Env
	err = cmd.Start()
	if err != nil {
		return 0, err
	}

	pid = int32(cmd.Process.Pid)

	go func() {
		stdoutScanner := bufio.NewScanner(stdoutPipe)
		stderrScanner := bufio.NewScanner(stderrPipe)

		for stdoutScanner.Scan() {
			outputFile.WriteString(stdoutScanner.Text() + "\n")
		}
		if err := stdoutScanner.Err(); err != nil {
			s.logger.Err(err).Msg("Error reading stdout")
		}

		for stderrScanner.Scan() {
			outputFile.WriteString(stderrScanner.Text() + "\n")
		}
		if err := stderrScanner.Err(); err != nil {
			s.logger.Err(err).Msg("Error reading stderr")
		}
	}()

	go func() {
		defer outputFile.Close()
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
		state, _ := s.getState(ctx, args.JID)
		if err != nil {
			s.logger.Info().Err(err).Int32("PID", pid).Msg("process terminated")
		} else {
			s.logger.Info().Int32("status", 0).Int32("PID", pid).Msg("process terminated")
		}
		state.JobState = task.JobState_JOB_DONE
		s.updateState(ctx, args.JID, state)
		if err != nil {
			s.logger.Error().Err(err).Msg("failed to update state after job done")
		}
	}()

	ppid := int32(os.Getpid())

	closeCommonFds(ppid, pid)
	return pid, err
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

	_, uploadSpan := s.tracer.Start(ctx, "upload-ckpt")
	multipartCheckpointResp, cid, err := s.store.CreateMultiPartUpload(ctx, checkpointFullSize)
	if err != nil {
		st := status.New(codes.Internal, fmt.Sprintf("CreateMultiPartUpload failed with error: %s", err.Error()))
		uploadSpan.RecordError(err)
		return "", "", st.Err()
	}

	err = s.store.StartMultiPartUpload(ctx, cid, multipartCheckpointResp, path)
	if err != nil {
		st := status.New(codes.Internal, fmt.Sprintf("StartMultiPartUpload failed with error: %s", err.Error()))
		uploadSpan.RecordError(err)
		return "", "", st.Err()
	}

	err = s.store.CompleteMultiPartUpload(ctx, *multipartCheckpointResp, cid)
	if err != nil {
		st := status.New(codes.Internal, fmt.Sprintf("CompleteMultiPartUpload failed with error: %s", err.Error()))
		uploadSpan.RecordError(err)
		return "", "", st.Err()
	}
	uploadSpan.End()

	return cid, multipartCheckpointResp.UploadID, err
}

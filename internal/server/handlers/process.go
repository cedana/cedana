package handlers

// Defines process handlers that ship with the server

import (
	"context"
	"os"
	"os/exec"
	"sync"
	"syscall"

	"github.com/cedana/cedana/pkg/api/daemon"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	LOG_FILE_PERMS os.FileMode = 0644
	LOG_FILE_FLAGS int         = os.O_CREATE | os.O_WRONLY | os.O_APPEND | os.O_TRUNC
)

// Run starts a process with the given options and returns a channel that will receive the exit code of the process
func Run(ctx context.Context, lifetimeCtx context.Context, wg *sync.WaitGroup, resp *daemon.StartResp, req *daemon.StartReq) (chan int, error) {
	opts := req.GetDetails().GetProcessStart()
	if opts == nil {
		return nil, status.Error(codes.InvalidArgument, "missing process start options")
	}
	if opts.Path == "" {
		return nil, status.Error(codes.InvalidArgument, "missing path")
	}

	groupsUint32 := make([]uint32, len(opts.Groups))
	for i, v := range opts.Groups {
		groupsUint32[i] = uint32(v)
	}
	cmd := exec.CommandContext(lifetimeCtx, opts.Path, opts.Args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
		Credential: &syscall.Credential{
			Uid:    uint32(opts.UID),
			Gid:    uint32(opts.GID),
			Groups: groupsUint32,
		},
	}
	cmd.Env = opts.Env
	cmd.Dir = opts.WorkingDir

	logFile, err := os.OpenFile(req.Log, LOG_FILE_FLAGS, LOG_FILE_PERMS)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to open log file: %v", err)
	}
	defer logFile.Close()
	cmd.Stdin = logFile
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	err = cmd.Start()
	if err != nil {
		return nil, err
	}

	resp.PID = uint32(cmd.Process.Pid)

	// Wait for the process to exit, send exit code
	exited := make(chan int)
	wg.Add(1)
	go func() {
		defer wg.Done()
		err := cmd.Wait()
		if err != nil {
			log.Debug().Err(err).Msg("process Wait()")
		}
		log.Debug().Int("code", cmd.ProcessState.ExitCode()).Msg("process exited")
		close(exited)
	}()

	return exited, err
}

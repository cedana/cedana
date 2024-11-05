package handlers

// Defines process handlers that ship with the server

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/cedana/cedana/pkg/api/daemon"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	OUTPUT_FILE_PATH_FORMATTER string      = "/var/log/cedana-output-%d.log"
	OUTPUT_FILE_PERMS          os.FileMode = 0644
	OUTPUT_FILE_FLAGS          int         = os.O_CREATE | os.O_WRONLY | os.O_APPEND | os.O_TRUNC
)

// Run starts a process with the given options and returns a channel that will receive the exit code of the process
func Run(ctx context.Context, lifetimeCtx context.Context, wg *sync.WaitGroup, resp *daemon.StartResp, req *daemon.StartReq) (chan int, error) {
	opts := req.GetDetails().GetProcessStartOpts()
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

	outFilePath := fmt.Sprintf(OUTPUT_FILE_PATH_FORMATTER, time.Now().Unix())
	outFile, err := os.OpenFile(outFilePath, OUTPUT_FILE_FLAGS, OUTPUT_FILE_PERMS)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to open output file: %v", err)
	}
	defer outFile.Close()
	cmd.Stdin = outFile
	cmd.Stdout = outFile
	cmd.Stderr = outFile

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

package handlers

// Defines run (process) handlers that ship with the server

import (
	"context"
	"math/rand"
	"os"
	"os/exec"
	"syscall"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/keys"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Run starts a process with the given options and returns a channel that will receive the exit code of the process
func Run() types.Run {
	return func(ctx context.Context, server types.ServerOpts, resp *daemon.RunResp, req *daemon.RunReq) (exited chan int, err error) {
		opts := req.GetDetails().GetProcessRun()
		if opts == nil {
			return nil, status.Error(codes.InvalidArgument, "missing process run options")
		}
		if opts.Path == "" {
			return nil, status.Error(codes.InvalidArgument, "missing path")
		}

		cmd := exec.CommandContext(server.Lifetime, opts.Path, opts.Args...)
		cmd.SysProcAttr = &syscall.SysProcAttr{
			Setsid: true,
			Credential: &syscall.Credential{
				Uid:    uint32(opts.UID),
				Gid:    uint32(opts.GID),
				Groups: utils.Uint32Slice(opts.Groups),
			},
			Pdeathsig: syscall.SIGKILL, // kill even if server dies suddenly
		}
		cmd.Env = opts.Env
		cmd.Dir = opts.WorkingDir

		// Attach IO if requested, otherwise log to file
		exitCode := make(chan int, 1)
		if req.Attachable {
			// Use a random number, since we don't have PID yet
			id := rand.Uint32()
			stdIn, stdOut, stdErr := utils.NewStreamIOSlave(server.Lifetime, id, exitCode)
			defer utils.SetIOSlavePID(id, &resp.PID) // PID should be available then
			cmd.Stdin = stdIn
			cmd.Stdout = stdOut
			cmd.Stderr = stdErr
		} else {
			logFile, ok := ctx.Value(keys.RUN_LOG_FILE_CONTEXT_KEY).(*os.File)
			if !ok {
				return nil, status.Errorf(codes.Internal, "failed to get log file from context")
			}
			cmd.Stdin = nil // /dev/null
			cmd.Stdout = logFile
			cmd.Stderr = logFile
		}

		err = cmd.Start()
		if err != nil {
			return nil, err
		}

		resp.PID = uint32(cmd.Process.Pid)

		// Wait for the process to exit, send exit code
		exited = make(chan int)
		server.WG.Add(1)
		go func() {
			defer server.WG.Done()
			err := cmd.Wait()
			if err != nil {
				log.Trace().Err(err).Uint32("PID", resp.PID).Msg("process Wait()")
			}
			code := cmd.ProcessState.ExitCode()
			log.Debug().Int("code", code).Uint32("PID", resp.PID).Msg("process exited")
			exitCode <- code
			close(exitCode)
			close(exited)
		}()

		return exited, err
	}
}

package process

import (
	"context"
	"os"
	"os/exec"
	"syscall"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/channel"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/mattn/go-isatty"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Run starts a process with the given options and returns a channel that will receive the exit code of the process
func Run(ctx context.Context, opts types.Opts, resp *daemon.RunResp, req *daemon.RunReq) (code func() <-chan int, err error) {
	details := req.GetDetails().GetProcess()
	if details == nil {
		return nil, status.Error(codes.InvalidArgument, "missing process run options")
	}
	if details.Path == "" {
		return nil, status.Error(codes.InvalidArgument, "missing path")
	}

	cmd := exec.Command(details.Path, details.Args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
		Credential: &syscall.Credential{
			Uid:    req.UID,
			Gid:    req.GID,
			Groups: req.Groups,
		},
	}
	cmd.Env = req.Env
	cmd.Dir = details.WorkingDir

	exitCode := make(chan int, 1)
	code = channel.Broadcaster(exitCode)

	cmd.Stdin = opts.IO.Stdin
	cmd.Stdout = opts.IO.Stdout
	cmd.Stderr = opts.IO.Stderr

	if opts.Serverless {
		cmd.SysProcAttr.Setsid = false                     // Use the current session
		cmd.SysProcAttr.GidMappingsEnableSetgroups = false // Avoid permission issues when running as non-root user
		cmd.SysProcAttr.Credential = nil                   // Current user's credentials (caller)
		if isatty.IsTerminal(os.Stdin.Fd()) {
			cmd.SysProcAttr.Foreground = isatty.IsTerminal(os.Stdin.Fd()) // Run in the foreground to catch signals
			cmd.SysProcAttr.Ctty = int(os.Stdin.Fd())                     // Set the controlling terminal
		}
	}

	err = cmd.Start()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to start process: %v", err)
	}

	resp.PID = uint32(cmd.Process.Pid)

	opts.WG.Go(func() {
		err := cmd.Wait()
		if err != nil {
			log.Trace().Err(err).Uint32("PID", resp.PID).Msg("process Wait()")
		}
		exitCode <- cmd.ProcessState.ExitCode()
		close(exitCode)
	})

	return code, nil
}

// Simply sets the PID in response, if the process exists, and returns a valid exited channel
func Manage(ctx context.Context, opts types.Opts, resp *daemon.RunResp, req *daemon.RunReq) (code func() <-chan int, err error) {
	details := req.GetDetails().GetProcess()
	if details == nil {
		return nil, status.Error(codes.InvalidArgument, "missing process run options")
	}
	if details.PID == 0 {
		return nil, status.Error(codes.InvalidArgument, "missing PID")
	}

	switch req.Action {
	case daemon.RunAction_MANAGE_EXISTING:
		if !utils.PidExists(details.PID) {
			return nil, status.Errorf(codes.NotFound, "process with PID %d does not exist", details.PID)
		}
	case daemon.RunAction_MANAGE_UPCOMING:
		// Not possible for linux processes, as you cannot create a process with a specific PID
		return nil, status.Errorf(codes.InvalidArgument, "manage upcoming is not supported for linux processes")
	}

	resp.PID = details.PID

	return channel.Broadcaster(utils.WaitForPid(details.PID)), nil
}

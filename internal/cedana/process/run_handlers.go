package process

import (
	"context"
	"math/rand"
	"os"
	"os/exec"
	"syscall"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/channel"
	cedana_io "github.com/cedana/cedana/pkg/io"
	"github.com/cedana/cedana/pkg/keys"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/mattn/go-isatty"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var (
	Run    types.Run = run
	Manage types.Run = manage
)

// Run starts a process with the given options and returns a channel that will receive the exit code of the process
func run(ctx context.Context, opts types.Opts, resp *daemon.RunResp, req *daemon.RunReq) (code func() <-chan int, err error) {
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

	daemonless, _ := ctx.Value(keys.DAEMONLESS_CONTEXT_KEY).(bool)
	if daemonless {
		cmd.SysProcAttr.Setsid = false                     // Use the current session
		cmd.SysProcAttr.GidMappingsEnableSetgroups = false // Avoid permission issues when running as non-root user
		cmd.SysProcAttr.Credential = nil                   // Current user's credentials (caller)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if isatty.IsTerminal(os.Stdin.Fd()) {
			cmd.SysProcAttr.Foreground = isatty.IsTerminal(os.Stdin.Fd()) // Run in the foreground to catch signals
			cmd.SysProcAttr.Ctty = int(os.Stdin.Fd())                     // Set the controlling terminal
		}
	} else if req.Attachable {
		// Use a random number, since we don't have PID yet
		id := rand.Uint32()
		stdIn, stdOut, stdErr := cedana_io.NewStreamIOSlave(opts.Lifetime, opts.WG, id, code())
		defer cedana_io.SetIOSlavePID(id, &resp.PID) // PID should be available then
		cmd.Stdin = stdIn
		cmd.Stdout = stdOut
		cmd.Stderr = stdErr
	} else {
		logFile, ok := ctx.Value(keys.OUT_FILE_CONTEXT_KEY).(*os.File)
		if !ok {
			return nil, status.Errorf(codes.Internal, "failed to get log file from context")
		}
		cmd.Stdin = nil // /dev/null
		cmd.Stdout = logFile
		cmd.Stderr = logFile
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
func manage(ctx context.Context, opts types.Opts, resp *daemon.RunResp, req *daemon.RunReq) (code func() <-chan int, err error) {
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

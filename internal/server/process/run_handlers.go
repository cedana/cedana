package process

import (
	"context"
	"math/rand"
	"os"
	"os/exec"
	"syscall"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	cedana_io "github.com/cedana/cedana/pkg/io"
	"github.com/cedana/cedana/pkg/keys"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var (
	Run    types.Run = run
	Manage types.Run = manage
)

// Run starts a process with the given options and returns a channel that will receive the exit code of the process
func run(ctx context.Context, opts types.Opts, resp *daemon.RunResp, req *daemon.RunReq) (exited chan int, err error) {
	details := req.GetDetails().GetProcess()
	if details == nil {
		return nil, status.Error(codes.InvalidArgument, "missing process run options")
	}
	if details.Path == "" {
		return nil, status.Error(codes.InvalidArgument, "missing path")
	}

	cmd := exec.CommandContext(opts.Lifetime, details.Path, details.Args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
		Credential: &syscall.Credential{
			Uid:    details.UID,
			Gid:    details.GID,
			Groups: details.Groups,
		},
		// Pdeathsig: syscall.SIGKILL, // kill even if server dies suddenly
		// XXX: Above is commented out because if we try to restore a managed job,
		// one that was started by the daemon,
		// using a dump path (directly w/ restore -p <path>), instead of using job
		// restore, the restored process dies immediately.
	}
	cmd.Env = details.Env
	cmd.Dir = details.WorkingDir

	// Attach IO if requested, otherwise log to file
	exitCode := make(chan int, 1)
	if req.Attachable {
		// Use a random number, since we don't have PID yet
		id := rand.Uint32()
		stdIn, stdOut, stdErr := cedana_io.NewStreamIOSlave(opts.Lifetime, opts.WG, id, exitCode)
		defer cedana_io.SetIOSlavePID(id, &resp.PID) // PID should be available then
		cmd.Stdin = stdIn
		cmd.Stdout = stdOut
		cmd.Stderr = stdErr
	} else {
		logFile, ok := ctx.Value(keys.LOG_FILE_CONTEXT_KEY).(*os.File)
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

	// Wait for the process to exit, send exit code
	exited = make(chan int)
	opts.WG.Add(1)
	go func() {
		defer opts.WG.Done()
		err := cmd.Wait()
		if err != nil {
			log.Trace().Err(err).Uint32("PID", resp.PID).Msg("process Wait()")
		}
		code := cmd.ProcessState.ExitCode()
		log.Debug().Int("code", code).Uint8("PID", uint8(resp.PID)).Msg("process exited")
		exitCode <- code
		close(exitCode)
		close(exited)
	}()

	return exited, nil
}

// Simply sets the PID in response, if the process exists, and returns a valid exited channel
func manage(ctx context.Context, opts types.Opts, resp *daemon.RunResp, req *daemon.RunReq) (exited chan int, err error) {
	details := req.GetDetails().GetProcess()
	if details == nil {
		return nil, status.Error(codes.InvalidArgument, "missing process run options")
	}
	if details.PID == 0 {
		return nil, status.Error(codes.InvalidArgument, "missing PID")
	}

	if !utils.PidExists(details.PID) {
		return nil, status.Errorf(codes.NotFound, "process with PID %d does not exist", details.PID)
	}

	resp.PID = details.PID

	return utils.WaitForPid(details.PID), nil
}

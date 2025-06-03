package container

// Defines run (runc) handlers that ship with this plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"

	cedana_io "github.com/cedana/cedana/pkg/io"
	"github.com/cedana/cedana/pkg/keys"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/cedana/cedana/plugins/runc/internal/defaults"
	runc_keys "github.com/cedana/cedana/plugins/runc/pkg/keys"
	"github.com/cedana/cedana/plugins/runc/pkg/runc"
	"github.com/opencontainers/runc/libcontainer"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	RUNC_BINARY    = "runc"
	RUNC_LOG_FILE  = "runc.log"
	RUNC_LOG_DEBUG = false

	waitForRunErrTimeout         = 2 * time.Second
	waitForManageUpcomingTimeout = 2 * time.Minute
)

type RuncState struct {
	ID  string `json:"id"`
	PID int    `json:"pid"`
}

type RuncLogMsg struct {
	Msg   string `json:"msg"`
	Level string `json:"level"`
	Time  string `json:"time"`
}

func RuncLogMsgToString(b []byte) (string, error) {
	var msg RuncLogMsg
	err := json.Unmarshal(b, &msg)
	if err != nil {
		return "", err
	}
	return msg.Msg, nil
}

var (
	Run    types.Run = run
	Manage types.Run = manage
)

// run runs a container using CLI directly
func run(ctx context.Context, opts types.Opts, resp *daemon.RunResp, req *daemon.RunReq) (exited chan int, err error) {
	details := req.GetDetails().GetRunc()
	root := details.GetRoot()
	id := details.GetID()
	bundle := details.GetBundle()
	noPivot := details.GetNoPivot()
	noNewKeyring := details.GetNoNewKeyring()
	detach := details.GetDetach()

	spec, ok := ctx.Value(runc_keys.SPEC_CONTEXT_KEY).(*specs.Spec)
	if !ok {
		return nil, status.Errorf(codes.Internal, "failed to get spec from context")
	}

	daemonless, _ := ctx.Value(keys.DAEMONLESS_CONTEXT_KEY).(bool)

	if !daemonless {
		spec.Process.Terminal = false // force pass-through terminal, since we're managing it
		detach = true                 // always detach when we are managing IO
	}

	// Apply updated spec to the bundle
	configFile := filepath.Join(bundle, runc.SpecConfigFile)
	err = runc.UpdateSpec(configFile, spec)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to apply spec: %v", err)
	}
	log.Trace().Interface("spec", spec).Str("runc", id).Msg("updated spec, backing up old spec")
	defer runc.RestoreSpec(configFile)

	logFile := filepath.Join(bundle, RUNC_LOG_FILE)
	pidFile := filepath.Join(os.TempDir(), fmt.Sprintf("cedana-runc-%s.pid", id))

	os.Remove(logFile)
	os.Remove(pidFile)

	cmd := exec.CommandContext(ctx,
		RUNC_BINARY,
		fmt.Sprintf("--root=%s", root),
		fmt.Sprintf("--log=%s", logFile),
		fmt.Sprintf("--log-format=%s", "json"),
		fmt.Sprintf("--debug=%t", RUNC_LOG_DEBUG),
		"run",
		fmt.Sprintf("--detach=%t", detach),
		fmt.Sprintf("--no-pivot=%t", noPivot),
		fmt.Sprintf("--no-new-keyring=%t", noNewKeyring),
		fmt.Sprintf("--pid-file=%s", pidFile),
		id,
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid:     true,
		Credential: &syscall.Credential{Uid: req.UID, Gid: req.GID, Groups: req.Groups},
	}
	cmd.Env = req.Env
	cmd.Dir = bundle

	// Attach IO if requested, otherwise log to file
	exitCode := make(chan int, 1)
	if daemonless {
		cmd.SysProcAttr.Setsid = false                     // We want to run in the foreground
		cmd.SysProcAttr.Foreground = true                  // Run in the foreground, so IO and signals work correctly
		cmd.SysProcAttr.Ctty = int(os.Stdin.Fd())          // Set the controlling terminal to the current stdin
		cmd.SysProcAttr.GidMappingsEnableSetgroups = false // Avoid permission issues when running as non-root user
		cmd.SysProcAttr.Credential = nil                   // Current user's credentials (caller)
		if !detach {
			cmd.Stdin = os.Stdin
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
		}
	} else if req.Attachable {
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
		return nil, status.Errorf(codes.Internal, "failed to start container: %v", err)
	}

	if !daemonless {
		defer func() {
			if err != nil {
				// Don't use cmd.Wait() as it will indefinitely wait for IO to complete
				cmd.Process.Wait()
			}
		}()

		// Wait for PID file to be created, until the process exists
		utils.WaitForFile(ctx, pidFile, utils.WaitForPid(uint32(cmd.Process.Pid)))
	}

	// Capture logs from runc
	lastMsg := utils.LogFromFile(
		log.With().
			Str("spec", spec.Version).
			Logger().WithContext(ctx),
		logFile,
		zerolog.TraceLevel,
		RuncLogMsgToString,
	)

	var container *libcontainer.Container

	if !daemonless {
		container, err = libcontainer.Load(root, id)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to load container: %v: %s", err, lastMsg)
		}
		state, err := container.State()
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to get container state: %v: %s", err, lastMsg)
		}

		resp.PID = uint32(state.InitProcessPid)
	} else {
		resp.PID = uint32(cmd.Process.Pid)
	}

	if daemonless {
		exited = exitCode // In daemonless mode, we return the exit code channel directly
	} else {
		exited = make(chan int, 1)
	}

	opts.WG.Add(1)
	go func() {
		defer opts.WG.Done()
		p, _ := os.FindProcess(int(resp.PID)) // always succeeds on linux
		status, err := p.Wait()
		if err != nil {
			log.Debug().Err(err).Msg("runc container Wait()")
		}
		code := status.ExitCode()
		log.Debug().Uint8("code", uint8(code)).Msg("runc container exited")

		cmd.Wait() // IO should be complete by now
		if !daemonless {
			container.Destroy()
		}

		exitCode <- code
		close(exited)
		if exitCode != exited {
			close(exitCode)
		}
	}()

	if !daemonless {
		// Also kill the container if lifetime expires
		opts.WG.Add(1)
		go func() {
			defer opts.WG.Done()
			<-opts.Lifetime.Done()
			syscall.Kill(int(resp.PID), syscall.SIGKILL)
		}()
	}

	return exited, nil
}

// manage simply sets the PID in response, if the container exists, and returns a valid exited channel
func manage(ctx context.Context, opts types.Opts, resp *daemon.RunResp, req *daemon.RunReq) (exited chan int, err error) {
	details := req.GetDetails().GetRunc()
	if details == nil {
		return nil, status.Error(codes.InvalidArgument, "missing runc options")
	}
	if details.ID == "" {
		return nil, status.Error(codes.InvalidArgument, "missing ID")
	}

	root := details.Root
	id := details.ID

	if root == "" {
		root = defaults.DEFAULT_ROOT
	}

	var container *libcontainer.Container

	switch req.Action {
	case daemon.RunAction_MANAGE_EXISTING:
		container, err = libcontainer.Load(root, id)
		if err != nil {
			return nil, status.Errorf(codes.NotFound, "container with ID %s does not exist: %v", id, err)
		}
	case daemon.RunAction_MANAGE_UPCOMING: // wait until the container is created
	loop:
		for {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-opts.Lifetime.Done():
				return nil, opts.Lifetime.Err()
			case <-time.After(500 * time.Millisecond):
				container, err = libcontainer.Load(root, id)
				if err == nil {
					break loop
				}
				log.Trace().Str("id", id).Msg("waiting for upcoming container to start managing")
			case <-time.After(waitForManageUpcomingTimeout):
				return nil, status.Errorf(codes.DeadlineExceeded, "timed out waiting for upcoming container %s", id)
			}
		}
	}

	state, err := container.State()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get container state: %v", err)
	}

	resp.PID = uint32(state.InitProcessPid)

	return utils.WaitForPid(resp.PID), nil
}

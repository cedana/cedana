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
	"strconv"
	"syscall"
	"time"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"

	"github.com/cedana/cedana/pkg/channel"
	cedana_io "github.com/cedana/cedana/pkg/io"
	"github.com/cedana/cedana/pkg/keys"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/cedana/cedana/plugins/runc/internal/defaults"
	runc_keys "github.com/cedana/cedana/plugins/runc/pkg/keys"
	"github.com/cedana/cedana/plugins/runc/pkg/logging"
	"github.com/cedana/cedana/plugins/runc/pkg/runc"
	"github.com/mattn/go-isatty"
	"github.com/opencontainers/runc/libcontainer"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	RUNC_BINARY   = "runc"
	RUNC_LOG_FILE = "runc.log"

	waitForRunErrTimeout         = 2 * time.Second
	waitForManageUpcomingTimeout = 2 * time.Minute
)

var RUNC_LOG_DEBUG = log.Logger.GetLevel() <= zerolog.DebugLevel

type RuncState struct {
	ID  string `json:"id"`
	PID int    `json:"pid"`
}

func RuncLogMsgToString(b []byte) (string, error) {
	var line logging.Line
	err := json.Unmarshal(b, &line)
	if err != nil {
		return "", err
	}
	return line.Msg, nil
}

var (
	Run    types.Run = run
	Manage types.Run = manage
)

// run runs a container using CLI directly
func run(ctx context.Context, opts types.Opts, resp *daemon.RunResp, req *daemon.RunReq) (code func() <-chan int, err error) {
	details := req.GetDetails().GetRunc()
	root := details.GetRoot()
	id := details.GetID()
	bundle := details.GetBundle()
	noPivot := details.GetNoPivot()
	noNewKeyring := details.GetNoNewKeyring()
	consoleSocket := details.GetConsoleSocket()
	detach := details.GetDetach()
	rootless := details.GetRootless()
	systemdCgroup := details.GetSystemdCgroup()
	noSubreaper := details.GetNoSubreaper()
	preserveFds := details.GetPreserveFDs()

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

	cmd := exec.Command(
		RUNC_BINARY,
		fmt.Sprintf("--root=%s", root),
		fmt.Sprintf("--log=%s", logFile),
		fmt.Sprintf("--log-format=%s", "json"),
		fmt.Sprintf("--debug=%t", RUNC_LOG_DEBUG),
		fmt.Sprintf("--systemd-cgroup=%t", systemdCgroup),
		fmt.Sprintf("--rootless=%s", rootless),
		"run",
		fmt.Sprintf("--detach=%t", detach),
		fmt.Sprintf("--no-pivot=%t", noPivot),
		fmt.Sprintf("--no-new-keyring=%t", noNewKeyring),
		fmt.Sprintf("--pid-file=%s", pidFile), // only used for synchronization, we have our own PID file flag
		fmt.Sprintf("--console-socket=%s", consoleSocket),
		fmt.Sprintf("--no-subreaper=%t", noSubreaper),
		fmt.Sprintf("--preserve-fds=%d", preserveFds),
		id,
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid:     true,
		Credential: &syscall.Credential{Uid: req.UID, Gid: req.GID, Groups: req.Groups},
	}
	cmd.Env = req.Env
	cmd.Dir = bundle

	exitCode := make(chan int, 1)
	code = channel.Broadcaster(exitCode)

	if daemonless {
		cmd.SysProcAttr.Setsid = false                     // We want to run in the foreground
		cmd.SysProcAttr.GidMappingsEnableSetgroups = false // Avoid permission issues when running as non-root user
		cmd.SysProcAttr.Credential = nil                   // Current user's credentials (caller)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if !detach && isatty.IsTerminal(os.Stdin.Fd()) {
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
		outFile, ok := ctx.Value(keys.OUT_FILE_CONTEXT_KEY).(*os.File)
		if !ok {
			return nil, status.Errorf(codes.Internal, "failed to get log file from context")
		}
		cmd.Stdin = nil // /dev/null
		cmd.Stdout = outFile
		cmd.Stderr = outFile
	}

	err = cmd.Start()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to start container: %v", err)
	}

	defer func() {
		if err != nil {
			// Don't use cmd.Wait() as it will indefinitely wait for IO to complete
			cmd.Process.Wait()
		}
	}()

	// Wait for PID file to be created, until the process exists
	pidData, err := utils.WaitForFile(ctx, pidFile, utils.WaitForPidCtx(ctx, uint32(cmd.Process.Pid)))
	if err != nil {
		lastMsg, _ := utils.LastMsgFromFile(logFile, RuncLogMsgToString)
		return nil, status.Error(codes.Internal, lastMsg)
	}

	pid, err := strconv.ParseUint(string(pidData), 10, 32)
	if err != nil {
		lastMsg, _ := utils.LastMsgFromFile(logFile, RuncLogMsgToString)
		return nil, status.Errorf(codes.Internal, "failed to parse PID from file: %v %s", err, lastMsg)
	}

	// Capture logs from runc
	utils.LogFromFile(
		log.With().
			Str("spec", spec.Version).
			Logger().WithContext(ctx),
		logFile,
		zerolog.TraceLevel,
		RuncLogMsgToString,
	)

	resp.PID = uint32(pid)

	var processToReap *os.Process
	if daemonless {
		processToReap = cmd.Process // the runc process itself
	} else {
		processToReap, _ = os.FindProcess(int(resp.PID)) // the container process
	}

	opts.WG.Add(1)
	go func() {
		defer opts.WG.Done()
		status, err := processToReap.Wait()
		if err != nil {
			log.Trace().Err(err).Msg("process Wait()")
		}
		cmd.Wait() // Wait for all IO to complete
		exitCode <- status.ExitCode()
		close(exitCode)
	}()

	return code, nil
}

// manage simply sets the PID in response, if the container exists, and returns a valid exited channel
func manage(ctx context.Context, opts types.Opts, resp *daemon.RunResp, req *daemon.RunReq) (code func() <-chan int, err error) {
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

	return channel.Broadcaster(utils.WaitForPidCtx(opts.Lifetime, resp.PID)), nil
}

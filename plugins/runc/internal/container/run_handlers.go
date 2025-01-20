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
	RUNC_LOG_DEBUG = true

	waitForRunErrTimeout = 500 * time.Millisecond
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

	spec, ok := ctx.Value(runc_keys.SPEC_CONTEXT_KEY).(*specs.Spec)
	if !ok {
		return nil, status.Errorf(codes.Internal, "failed to get spec from context")
	}

	spec.Process.Terminal = false // force pass-through terminal, since we're managing it

	// Apply updated spec to the bundle
	configFile := filepath.Join(bundle, runc.SpecConfigFile)
	err = runc.UpdateSpec(configFile, spec)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to apply spec: %v", err)
	}
	log.Trace().Interface("spec", spec).Str("runc", id).Msg("updated spec, backing up old spec")
	defer runc.RestoreSpec(configFile)

	os.Remove(RUNC_LOG_FILE)

	cmd := exec.CommandContext(ctx,
		RUNC_BINARY,
		fmt.Sprintf("--root=%s", root),
		fmt.Sprintf("--log=%s", RUNC_LOG_FILE),
		fmt.Sprintf("--log-format=%s", "json"),
		fmt.Sprintf("--debug=%t", RUNC_LOG_DEBUG),
		"run", "--detach",
		fmt.Sprintf("--no-pivot=%t", noPivot),
		fmt.Sprintf("--no-new-keyring=%t", noNewKeyring),
		id,
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{Uid: uint32(0), Gid: uint32(0)},
		// Pdeathsig: syscall.SIGKILL, // kill even if server dies suddenly
		// XXX: Above is commented out because if we try to restore a managed job,
		// one that was started by the daemon,
		// using a dump path (directly w/ restore -p <path>), instead of using job
		// restore, the restored process dies immediately.
	}
	cmd.Dir = bundle

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

	time.Sleep(waitForRunErrTimeout)

	// Capture logs from runc
	lastMsg := utils.LogFromFile(
		log.With().
			Str("spec", spec.Version).
			Logger().WithContext(ctx),
		filepath.Join(RUNC_LOG_FILE),
		zerolog.TraceLevel,
		RuncLogMsgToString,
	)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to start container: %v: %s", err, lastMsg)
	}

	container, err := libcontainer.Load(root, id)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to load container: %v: %s", err, lastMsg)
	}
	state, err := container.State()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get container state: %v: %s", err, lastMsg)
	}

	resp.PID = uint32(state.InitProcessPid)

	// Wait for the process to exit, send exit code
	exited = make(chan int)
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

		cmd.Wait()
		container.Destroy()

		exitCode <- code
		close(exitCode)
		close(exited)
	}()

	// Also kill the container if lifetime expires
	opts.WG.Add(1)
	go func() {
		defer opts.WG.Done()
		<-opts.Lifetime.Done()
		syscall.Kill(int(resp.PID), syscall.SIGKILL)
	}()

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

	container, err := libcontainer.Load(root, id)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "container with ID %s does not exist: %v", id, err)
	}

	state, err := container.State()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get container state: %v", err)
	}

	resp.PID = uint32(state.InitProcessPid)

	return utils.WaitForPid(resp.PID), nil
}

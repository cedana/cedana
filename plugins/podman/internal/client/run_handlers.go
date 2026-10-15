package client

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/channel"
	"github.com/cedana/cedana/pkg/types"
	cedana_utils "github.com/cedana/cedana/pkg/utils"
	podman_keys "github.com/cedana/cedana/plugins/podman/pkg/keys"
	"github.com/cedana/cedana/plugins/podman/pkg/utils"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const waitForManageUpcomingTimeout = 2 * time.Minute

// Creates and starts the container. Used for both run and restore, as
// cedana-oci-runtime handles restoring.
func Run[REQ, RESP any](ctx context.Context, opts types.Opts, resp *RESP, req *REQ) (code func() <-chan int, err error) {
	createOpts, ok := ctx.Value(podman_keys.CREATE_OPTS_CONTEXT_KEY).(*CreateOpts)
	if !ok {
		return nil, status.Errorf(codes.Internal, "failed to get create options from context")
	}
	details := types.Details(req)
	id := details.GetPodman().GetID()

	// Keep stdin open only if there's input to forward, as it's otherwise /dev/null
	interactive := opts.IO.Stdin != nil

	args := append([]string{"create"}, createOpts.Flags...)
	if interactive {
		args = append(args, "--interactive")
	}
	args = append(args, createOpts.Image)
	args = append(args, createOpts.Args...)

	_, err = utils.Podman(ctx, args...)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create container: %v", err)
	}
	defer func() {
		if err != nil {
			Cleanup(context.WithoutCancel(ctx), details)
		}
	}()

	log.Debug().Str("id", id).Msg("created container")

	_, err = utils.Podman(ctx, "start", id)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to start container: %v", err)
	}

	containers, err := utils.Inspect(ctx, id)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to inspect container: %v", err)
	}
	pid := uint32(containers[0].State.Pid)
	if pid == 0 {
		// May have already exited, so get the PID it had from the runtime's pid file
		if data, err := os.ReadFile(containers[0].PidFile); err == nil {
			p, _ := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 32)
			pid = uint32(p)
		}
	}
	if pid == 0 {
		logs, _ := utils.Podman(ctx, "logs", "--tail", "10", id) // container is removed on error, so surface its last output
		return nil, status.Errorf(codes.Internal, "container %s exited (code %d): %s", id, containers[0].State.ExitCode, strings.TrimSpace(string(logs)))
	}

	types.SetPID(resp, pid)

	// Forward container input/output, if anyone's listening. Output is read from
	// the container logs, as attaching after start would miss early output.

	if interactive {
		attach := exec.CommandContext(opts.Lifetime, utils.PODMAN_BINARY, "attach", "--sig-proxy=false", id)
		attach.Stdin = opts.IO.Stdin
		if err := attach.Start(); err != nil {
			log.Warn().Err(err).Str("id", id).Msg("failed to attach to container stdin")
		} else {
			opts.WG.Go(func() { attach.Wait() })
		}
	}

	if opts.IO.Stdout != nil || opts.IO.Stderr != nil {
		logs := exec.CommandContext(opts.Lifetime, utils.PODMAN_BINARY, "logs", "--follow", id)
		logs.Stdout = opts.IO.Stdout
		logs.Stderr = opts.IO.Stderr
		if err := logs.Start(); err != nil {
			log.Warn().Err(err).Str("id", id).Msg("failed to follow container logs")
		} else {
			opts.WG.Go(func() { logs.Wait() })
		}
	}

	// Wait for the container to exit, send exit code. Stops waiting when the
	// daemon's lifetime ends, as podman (conmon) keeps the container running.

	exitCode := make(chan int, 1)
	code = channel.Broadcaster(exitCode)

	opts.WG.Go(func() {
		defer close(exitCode)
		out, err := utils.Podman(opts.Lifetime, "wait", id)
		if err != nil {
			log.Trace().Err(err).Uint32("PID", pid).Msg("container wait")
			return
		}
		status, err := strconv.Atoi(strings.TrimSpace(string(out)))
		if err != nil {
			log.Trace().Err(err).Uint32("PID", pid).Msg("container wait")
			return
		}
		exitCode <- status
	})

	return code, nil
}

func Manage(ctx context.Context, opts types.Opts, resp *daemon.RunResp, req *daemon.RunReq) (code func() <-chan int, err error) {
	details := req.GetDetails().GetPodman()

	var container *utils.Container

	switch req.Action {
	case daemon.RunAction_MANAGE_EXISTING:
		containers, err := utils.Inspect(ctx, details.ID)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to load container: %v", err)
		}
		container = containers[0]
	case daemon.RunAction_MANAGE_UPCOMING:
		timeout := time.After(waitForManageUpcomingTimeout)
	loop:
		for {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-opts.Lifetime.Done():
				return nil, opts.Lifetime.Err()
			case <-timeout:
				return nil, status.Errorf(codes.DeadlineExceeded, "timed out waiting for upcoming container %s", details.ID)
			case <-time.After(500 * time.Millisecond):
				containers, err := utils.Inspect(ctx, details.ID)
				if err == nil && containers[0].State.Pid != 0 {
					container = containers[0]
					break loop
				}
				log.Trace().Str("id", details.ID).Msg("waiting for upcoming container to start managing")
			}
		}
	}

	if container.State.Pid == 0 {
		return nil, status.Errorf(codes.FailedPrecondition, "container %s is not running", details.ID)
	}

	resp.PID = uint32(container.State.Pid)

	return channel.Broadcaster(cedana_utils.WaitForPid(resp.PID)), nil
}

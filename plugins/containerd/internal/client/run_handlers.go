package client

import (
	"context"
	"time"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/channel"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/pkg/utils"
	containerd_keys "github.com/cedana/cedana/plugins/containerd/pkg/keys"
	"github.com/containerd/containerd"
	"github.com/containerd/containerd/cio"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func Run[REQ, RESP any](ctx context.Context, opts types.Opts, resp *RESP, req *REQ) (code func() <-chan int, err error) {
	client, ok := ctx.Value(containerd_keys.CLIENT_CONTEXT_KEY).(*containerd.Client)
	if !ok {
		return nil, status.Errorf(codes.Internal, "failed to get client from context")
	}
	container, ok := ctx.Value(containerd_keys.CONTAINER_CONTEXT_KEY).(containerd.Container)
	if !ok {
		return nil, status.Errorf(codes.Internal, "failed to get container from context")
	}
	details := types.Details(req).GetContainerd()
	noPivot := details.GetNoPivot()

	exitCode := make(chan int, 1)
	code = channel.Broadcaster(exitCode)

	var pid uint32

	io := cio.WithStreams(opts.IO.Stdin, opts.IO.Stdout, opts.IO.Stderr)

	taskOptions := []containerd.NewTaskOpts{}
	if noPivot {
		taskOptions = append(taskOptions, containerd.WithNoPivotRoot)
	}

	task, err := container.NewTask(ctx, cio.NewCreator(io), taskOptions...)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create new task: %v", err)
	}

	err = task.Start(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to start task: %v", err)
	}

	pid = uint32(task.Pid())
	types.SetPID(resp, pid)

	// Wait for the container to exit, send exit code
	opts.WG.Go(func() {
		defer client.Close()
		statusChan, _ := task.Wait(context.WithoutCancel(opts.Lifetime))
		status := <-statusChan
		err = status.Error()
		if err != nil {
			log.Trace().Err(err).Uint32("PID", pid).Msg("container Wait()")
		}
		exitCode <- int(status.ExitCode())
		close(exitCode)
	})

	return code, nil
}

func Manage(ctx context.Context, opts types.Opts, resp *daemon.RunResp, req *daemon.RunReq) (code func() <-chan int, err error) {
	details := req.GetDetails().GetContainerd()

	client, ok := ctx.Value(containerd_keys.CLIENT_CONTEXT_KEY).(*containerd.Client)
	if !ok {
		return nil, status.Errorf(codes.FailedPrecondition, "failed to get containerd client from context")
	}

	var container containerd.Container

	switch req.Action {
	case daemon.RunAction_MANAGE_EXISTING:
		container, err = client.LoadContainer(ctx, details.ID)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to load container: %v", err)
		}
	case daemon.RunAction_MANAGE_UPCOMING:
	loop:
		for {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-opts.Lifetime.Done():
				return nil, opts.Lifetime.Err()
			case <-time.After(500 * time.Millisecond):
				container, err = client.LoadContainer(ctx, details.ID)
				if err == nil {
					break loop
				}
				log.Trace().Msg("waiting for upcoming container to start managing")
			case <-time.After(waitForManageUpcomingTimeout):
				return nil, status.Errorf(codes.DeadlineExceeded, "timed out waiting for upcoming container %s", details.ID)
			}
		}
	}

	task, err := container.Task(ctx, nil)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get task: %v", err)
	}

	resp.PID = uint32(task.Pid())

	return channel.Broadcaster(utils.WaitForPid(resp.PID)), nil
}

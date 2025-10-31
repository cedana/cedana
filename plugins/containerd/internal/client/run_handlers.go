package client

import (
	"context"
	"math/rand/v2"
	"os"
	"time"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/channel"
	cedana_io "github.com/cedana/cedana/pkg/io"
	"github.com/cedana/cedana/pkg/keys"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/pkg/utils"
	containerd_keys "github.com/cedana/cedana/plugins/containerd/pkg/keys"
	"github.com/containerd/containerd"
	"github.com/containerd/containerd/cio"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var (
	Run    types.Run = run
	Manage types.Run = manage
)

func run(ctx context.Context, opts types.Opts, resp *daemon.RunResp, req *daemon.RunReq) (code func() <-chan int, err error) {
	client, ok := ctx.Value(containerd_keys.CLIENT_CONTEXT_KEY).(*containerd.Client)
	if !ok {
		return nil, status.Errorf(codes.Internal, "failed to get client from context")
	}
	container, ok := ctx.Value(containerd_keys.CONTAINER_CONTEXT_KEY).(containerd.Container)
	if !ok {
		return nil, status.Errorf(codes.Internal, "failed to get container from context")
	}
	noPivot := req.GetDetails().GetContainerd().GetNoPivot()

	exitCode := make(chan int, 1)
	code = channel.Broadcaster(exitCode)

	var io cio.Opt
	if req.Attachable {
		// Use a random number, since we don't have PID yet
		id := rand.Uint32()
		stdIn, stdOut, stdErr := cedana_io.NewStreamIOSlave(opts.Lifetime, opts.WG, id, code())
		defer cedana_io.SetIOSlavePID(id, &resp.PID) // PID should be available then
		io = cio.WithStreams(stdIn, stdOut, stdErr)
	} else {
		outFile, ok := ctx.Value(keys.OUT_FILE_CONTEXT_KEY).(*os.File)
		if !ok {
			return nil, status.Errorf(codes.Internal, "failed to get log file from context")
		}
		io = cio.WithStreams(nil, outFile, outFile)
	}

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

	resp.PID = uint32(task.Pid())

	// Wait for the container to exit, send exit code
	opts.WG.Go(func() {
		defer client.Close()
		statusChan, _ := task.Wait(context.WithoutCancel(opts.Lifetime))
		status := <-statusChan
		err = status.Error()
		if err != nil {
			log.Trace().Err(err).Uint32("PID", resp.PID).Msg("container Wait()")
		}
		exitCode <- int(status.ExitCode())
		close(exitCode)
	})

	return code, nil
}

func manage(ctx context.Context, opts types.Opts, resp *daemon.RunResp, req *daemon.RunReq) (code func() <-chan int, err error) {
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

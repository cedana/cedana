package client

import (
	"context"
	"math/rand/v2"
	"os"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	cedana_io "github.com/cedana/cedana/pkg/io"
	"github.com/cedana/cedana/pkg/keys"
	"github.com/cedana/cedana/pkg/types"
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

func run(ctx context.Context, server types.ServerOpts, resp *daemon.RunResp, req *daemon.RunReq) (exited chan int, err error) {
	container, ok := ctx.Value(containerd_keys.CONTAINER_CONTEXT_KEY).(containerd.Container)
	if !ok {
		return nil, status.Errorf(codes.Internal, "failed to get container from context")
	}

	// Attach IO if requested, otherwise log to file
	exitCode := make(chan int, 1)
	var io cio.Opt
	if req.Attachable {
		// Use a random number, since we don't have PID yet
		id := rand.Uint32()
		stdIn, stdOut, stdErr := cedana_io.NewStreamIOSlave(server.Lifetime, server.WG, id, exitCode)
		defer cedana_io.SetIOSlavePID(id, &resp.PID) // PID should be available then
		io = cio.WithStreams(stdIn, stdOut, stdErr)
	} else {
		logFile, ok := ctx.Value(keys.LOG_FILE_CONTEXT_KEY).(*os.File)
		if !ok {
			return nil, status.Errorf(codes.Internal, "failed to get log file from context")
		}
		io = cio.WithStreams(nil, logFile, logFile)
	}

	task, err := container.NewTask(ctx, cio.NewCreator(io))
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get task: %v", err)
	}

	err = task.Start(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to start task: %v", err)
	}

	resp.PID = uint32(task.Pid())

	// Wait for the container to exit, send exit code
	exited = make(chan int)
	server.WG.Add(1)
	go func() {
		defer server.WG.Done()

		statusChan, err := task.Wait(context.WithoutCancel(ctx))
		if err != nil {
			log.Trace().Err(err).Uint32("PID", resp.PID).Msg("container Wait()")
		}
		status := <-statusChan
		code := status.ExitCode()
		log.Debug().Uint32("code", code).Uint8("PID", uint8(resp.PID)).Msg("container exited")
		exitCode <- int(code)
		container.Delete(ctx, containerd.WithSnapshotCleanup)
		close(exitCode)
		close(exited)
	}()

	return exited, nil
}

func manage(ctx context.Context, server types.ServerOpts, resp *daemon.RunResp, req *daemon.RunReq) (exited chan int, err error) {
	details := req.GetDetails().GetContainerd()

	client, ok := ctx.Value(containerd_keys.CLIENT_CONTEXT_KEY).(*containerd.Client)
	if !ok {
		return nil, status.Errorf(codes.FailedPrecondition, "failed to get containerd client from context")
	}

	container, err := client.LoadContainer(ctx, details.ID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to load container: %v", err)
	}

	task, err := container.Task(ctx, nil)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get task: %v", err)
	}

	resp.PID = uint32(task.Pid())

	exited = make(chan int)

	server.WG.Add(1)
	go func() {
		defer server.WG.Done()
		statusChan, err := task.Wait(context.WithoutCancel(ctx))
		if err != nil {
			log.Trace().Err(err).Uint32("PID", resp.PID).Msg("container Wait()")
		}
		<-statusChan
		close(exited)
	}()

	return exited, nil
}

package client

import (
	"context"
	"math/rand/v2"
	"os"
	"time"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
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

	"github.com/opencontainers/runtime-spec/specs-go"
)

var (
	Run    types.Run = run
	Manage types.Run = manage
)

func run(ctx context.Context, opts types.Opts, resp *daemon.RunResp, req *daemon.RunReq) (exited chan int, err error) {
	details := req.GetDetails().GetContainerd()
	client, ok := ctx.Value(containerd_keys.CLIENT_CONTEXT_KEY).(*containerd.Client)
	if !ok {
		return nil, status.Errorf(codes.Internal, "failed to get containerd client")
	}
	spec, ok := ctx.Value(containerd_keys.SPEC_CONTEXT_KEY).(*specs.Spec)
	if !ok {
		return nil, status.Errorf(codes.Internal, "failed to get container specs")
	}
	image, err := client.GetImage(ctx, details.Image)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get image: %v", err)
	}

	var cOpts []containerd.NewContainerOpts
	// note: the options and their order is important DO NOT change
	cOpts = append(cOpts, containerd.WithSnapshotter("overlayfs"))
	cOpts = append(cOpts, containerd.WithNewSnapshot(details.ID, image))
	cOpts = append(cOpts, containerd.WithSpec(spec))

	container, err := client.NewContainer(
		ctx,
		details.ID,
		cOpts...,
	)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create container: %v", err)
	}
	defer func() {
		if err != nil {
			container.Delete(ctx, containerd.WithSnapshotCleanup)
		}
	}()

	// Attach IO if requested, otherwise log to file
	exitCode := make(chan int, 1)
	var io cio.Opt
	if req.Attachable {
		// Use a random number, since we don't have PID yet
		id := rand.Uint32()
		stdIn, stdOut, stdErr := cedana_io.NewStreamIOSlave(opts.Lifetime, opts.WG, id, exitCode)
		defer cedana_io.SetIOSlavePID(id, &resp.PID) // PID should be available then
		io = cio.WithStreams(stdIn, stdOut, stdErr)
	} else {
		logFile, ok := ctx.Value(keys.LOG_FILE_CONTEXT_KEY).(*os.File)
		if !ok {
			return nil, status.Errorf(codes.Internal, "failed to get log file from context")
		}
		io = cio.WithStreams(nil, logFile, logFile)
	}

	// Start the container
	task, err := container.NewTask(ctx, cio.NewCreator(io))
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get task: %v", err)
	}

	err = task.Start(opts.Lifetime)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to start task: %v", err)
	}
	time.Sleep(10)

	resp.PID = uint32(task.Pid())

	// Wait for the container to exit, send exit code
	exited = make(chan int)
	opts.WG.Add(1)
	go func() {
		ctx := context.WithoutCancel(ctx)
		defer opts.WG.Done()
		defer close(exitCode)
		defer close(exited)
		// TODO: note this function might return, before the task actually exits
		statusChan, err := task.Wait(ctx)
		if err != nil {
			log.Debug().Err(err).Uint32("PID", resp.PID).Msg("container Wait()")
		}
		// this might hang
		status := <-statusChan
		code := status.ExitCode()
		log.Debug().Uint32("code", code).Uint8("PID", uint8(resp.PID)).Msg("container exited")
		exitCode <- int(code)
		container.Delete(ctx, containerd.WithSnapshotCleanup)
	}()

	return exited, nil
}

func manage(ctx context.Context, opts types.Opts, resp *daemon.RunResp, req *daemon.RunReq) (exited chan int, err error) {
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

	return utils.WaitForPid(resp.PID), nil
}

package gpu

import (
	"context"
	"fmt"
	"os"
	"strings"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/pkg/keys"
	"github.com/cedana/cedana/pkg/profiling"
	"github.com/cedana/cedana/pkg/types"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// Adapter that restores GPU support to the request.
func Restore(gpus Manager) types.Adapter[types.Restore] {
	return func(next types.Restore) types.Restore {
		return func(ctx context.Context, opts types.Opts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (chan int, error) {
			state := resp.GetState()

			if !state.GPUEnabled {
				return next(ctx, opts, resp, req)
			}

			if !opts.Plugins.IsInstalled("gpu") {
				return nil, status.Errorf(codes.FailedPrecondition, "Please install the GPU plugin to restore GPU support")
			}

			pid := make(chan uint32, 1)
			defer close(pid)

			_, end := profiling.StartTimingCategory(ctx, "gpu", gpus.Attach)
			id, err := gpus.Attach(ctx, pid)
			end()
			if err != nil {
				return nil, status.Errorf(codes.Internal, "failed to attach GPU: %v", err)
			}

			// Import GPU CRIU callbacks
			opts.CRIUCallback.Include(gpus.CRIUCallback(id))

			ctx = context.WithValue(ctx, keys.GPU_ID_CONTEXT_KEY, id)

			next = next.With(InheritFilesForRestore)

			exited, err := next(ctx, opts, resp, req)
			if err != nil {
				return nil, err
			}

			pid <- resp.PID

			log.Info().Uint32("PID", resp.PID).Str("controller", id).Msg("GPU support restored for process")

			return exited, nil
		}
	}
}

func InheritFilesForRestore(next types.Restore) types.Restore {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (chan int, error) {
		id, ok := ctx.Value(keys.GPU_ID_CONTEXT_KEY).(string)
		if !ok {
			return nil, status.Errorf(codes.Internal, "failed to get GPU ID from context")
		}

		state := resp.GetState()
		if state == nil {
			return nil, status.Errorf(codes.InvalidArgument, "missing state. it should have been set by the adapter")
		}

		extraFiles, ok := ctx.Value(keys.EXTRA_FILES_CONTEXT_KEY).([]*os.File)
		if !ok {
			extraFiles = []*os.File{}
		}

		// Open new GPU shm file
		shmFile, err := os.OpenFile(fmt.Sprintf(CONTROLLER_SHM_FILE_FORMATTER, id), os.O_RDWR, 0o777)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to open GPU shm file: %v", err)
		}
		defer shmFile.Close()

		extraFiles = append(extraFiles, shmFile)

		if req.Criu == nil {
			req.Criu = &criu.CriuOpts{}
		}

		req.Criu.InheritFd = append(req.Criu.InheritFd, &criu.InheritFd{
			Key: proto.String(strings.TrimPrefix(fmt.Sprintf(CONTROLLER_SHM_FILE_FORMATTER, state.GPUID), "/")),
			Fd:  proto.Int32(int32(2 + len(extraFiles))),
		})

		ctx = context.WithValue(ctx, keys.EXTRA_FILES_CONTEXT_KEY, extraFiles)

		return next(ctx, opts, resp, req)
	}
}

package gpu

import (
	"context"
	"strings"
	"syscall"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/keys"
	"github.com/cedana/cedana/pkg/profiling"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/go-criu/v7/crit"
	"github.com/cedana/go-criu/v7/crit/images/fdinfo"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// Adapter that restores GPU support to the request.
func Restore(gpus Manager) types.Adapter[types.Restore] {
	return func(next types.Restore) types.Restore {
		return func(ctx context.Context, opts types.Opts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (chan int, error) {
			if !resp.GetState().GetGPUEnabled() {
				return next(ctx, opts, resp, req)
			}

			if !opts.Plugins.IsInstalled("gpu") {
				return nil, status.Errorf(codes.FailedPrecondition, "Please install the GPU plugin to restore GPU support")
			}

			user := &syscall.Credential{
				Uid:    req.UID,
				Gid:    req.GID,
				Groups: req.Groups,
			}

			env := req.GetEnv()

			pid := make(chan uint32, 1)
			defer close(pid)

			_, end := profiling.StartTimingCategory(ctx, "gpu", gpus.Attach)
			id, err := gpus.Attach(ctx, user, pid, env...)
			end()
			if err != nil {
				return nil, status.Errorf(codes.Internal, "failed to attach GPU: %v", err)
			}

			// Import GPU CRIU callbacks
			opts.CRIUCallback.Include(gpus.CRIUCallback(id, req.Stream, req.Env...))

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

// Manipulates GPU files in image (shm) to inherit a new file
// HACK: This is required until CRIU supports --inherit-fd for shared memory files
func InheritFilesForRestore(next types.Restore) types.Restore {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (chan int, error) {
		id, ok := ctx.Value(keys.GPU_ID_CONTEXT_KEY).(string)
		if !ok {
			return nil, status.Errorf(codes.Internal, "failed to get GPU ID from context")
		}

		fileR, err := opts.DumpFs.Open("files.img")
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to open files.img for manipulation: %v", err)
		}
		defer fileR.Close()

		fileW, err := opts.DumpFs.Create("files-new.img")
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to create files-new.img for manipulation: %v", err)
		}
		defer fileW.Close()

		critter := crit.New(fileR, fileW, "", false, false)

		img, err := critter.Decode(&fdinfo.FileEntry{})
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to decode files.img for manipulation: %v", err)
		}

		// Find the regular file in /dev/shm, and update the shm file name

		for _, entry := range img.Entries {
			entry := entry.Message.(*fdinfo.FileEntry)
			name := entry.GetReg().GetName()
			if strings.HasPrefix(name, "/dev/shm/cedana-gpu.") {
				entry.Reg.Name = proto.String(strings.Split(name, ".")[0] + "." + id)
			}
		}

		err = critter.Encode(img)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to encode files.img after manipulation: %v", err)
		}

		err = opts.DumpFs.Rename("files-new.img", "files.img")
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to rename files-new.img to files.img: %v", err)
		}

		return next(ctx, opts, resp, req)
	}
}

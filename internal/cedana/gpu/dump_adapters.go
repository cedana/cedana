package gpu

import (
	"context"
	"fmt"
	"regexp"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// NOTE: Add any other known NVIDIA GPU mount paths here.
var NVIDIA_MOUNTS_PATTERN = regexp.MustCompile(
	`^(` +
		`/driver/nvidia/gpus|` +
		`/dev/nvidia\d+|` +
		`/nvidia|` +
		`/usr/bin/nvidia|` +
		`/usr/lib/firmware/nvidia|` +
		`/usr/lib/libcuda|` +
		`/usr/lib64/libcuda|` +
		`/usr/lib/libnvidia|` +
		`/usr/lib64/libnvidia|` +
		`/usr/lib/x86_64-linux-gnu/libnvidia|` +
		`/usr/lib64/x86_64-linux-gnu/libnvidia` +
		`)`,
)

// Adapter that adds GPU dump to the request.
func Dump(gpus Manager) types.Adapter[types.Dump] {
	return func(next types.Dump) types.Dump {
		return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (code func() <-chan int, err error) {
			state := resp.GetState()
			if state == nil {
				return nil, status.Errorf(codes.InvalidArgument, "missing state. at least PID is required in resp.state")
			}

			pid := state.GetPID()

			err = gpus.Sync(ctx)
			if err != nil {
				return nil, status.Errorf(codes.Internal, "failed to sync GPU manager: %v", err)
			}

			if !gpus.IsAttached(pid) {
				return next(ctx, opts, resp, req)
			}

			if !opts.Plugins.IsInstalled("gpu") {
				return nil, status.Errorf(codes.FailedPrecondition, "Please install the GPU plugin to dump with GPU support")
			}

			id := gpus.GetID(pid)

			state.GPUID = id
			state.GPUEnabled = true

			// Import GPU CRIU callbacks
			opts.CRIUCallback.Include(gpus.CRIUCallback(id))

			next = next.With(AddMountsForDump)

			return next(ctx, opts, resp, req)
		}
	}
}

// Adapter that tells CRIU about the external GPU mounts.
func AddMountsForDump(next types.Dump) types.Dump {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (code func() <-chan int, err error) {
		state := resp.GetState()
		if state == nil {
			return nil, status.Errorf(
				codes.InvalidArgument,
				"missing state. at least PID is required in resp.state",
			)
		}

		utils.WalkTree(state, "Mounts", "Children", func(m *daemon.Mount) bool {
			if NVIDIA_MOUNTS_PATTERN.MatchString(m.Root) {
				log.Debug().Str("root", m.Root).Str("mount_path", m.MountPoint).Msg("marking NVIDIA GPU mount as external")
				req.Criu.External = append(req.Criu.External, fmt.Sprintf("mnt[%s]:%s", m.MountPoint, m.MountPoint))
			}
			return true
		})

		return next(ctx, opts, resp, req)
	}
}

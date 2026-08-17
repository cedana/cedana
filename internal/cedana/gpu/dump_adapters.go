package gpu

import (
	"context"
	"fmt"
	"regexp"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/criu"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// NOTE: Add any other known NVIDIA GPU mount paths here.
var NVIDIA_MOUNTS_PATTERN = regexp.MustCompile(
	`^(` +
		`/nvidia|` +
		`/dev/nvidia\d+|` +
		`/driver/nvidia/gpus|` +
		`/run/nvidia|` +
		`/usr/bin/nv|` +
		`/usr/lib/firmware/nv|` +
		`/usr/lib(64)?/(x86_64-linux-gnu/|aarch64-linux-gnu/)?(libcuda|libnv)|` +
		`.*nvidia.*|` +
		`/etc/vulkan` +
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

			next = next.With(AddExternalMountsForDump)

			return next(ctx, opts, resp, req)
		}
	}
}

// Adapter that tells CRIU about the external GPU mounts.
func AddExternalMountsForDump(next types.Dump) types.Dump {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (code func() <-chan int, err error) {
		state := resp.GetState()
		if state == nil {
			return nil, status.Errorf(
				codes.InvalidArgument,
				"missing state. at least PID is required in resp.state",
			)
		}

		initial := gpuExternalMountKeys(state)
		req.Criu.External = append(req.Criu.External, initial...)

		pid := state.PID
		opts.CRIUCallback.Include(&criu.NotifyCallback{
			Name: "gpu external mounts",
			QueryExtFilesFunc: func(ctx context.Context) ([]string, error) {
				frozen := &daemon.ProcessState{}
				if err := utils.FillProcessState(ctx, pid, frozen, true); err != nil {
					return nil, err
				}
				return gpuExternalMountKeys(frozen), nil
			},
		})

		return next(ctx, opts, resp, req)
	}
}

func gpuExternalMountKeys(state *daemon.ProcessState) []string {
	var keys []string
	utils.WalkTree(state, "Mounts", "Children", func(m *daemon.Mount) bool {
		if NVIDIA_MOUNTS_PATTERN.MatchString(m.Root) {
			log.Trace().Interface("m", m).Msg("marking NVIDIA GPU mount as external")
			keys = append(keys, fmt.Sprintf("mnt[%s]:%s", m.MountPoint, m.MountPoint))
		}
		return true
	})
	return keys
}

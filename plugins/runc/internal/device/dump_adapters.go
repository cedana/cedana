package device

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/plugins/runc/internal/filesystem"
	runc_keys "github.com/cedana/cedana/plugins/runc/pkg/keys"
	"github.com/opencontainers/runc/libcontainer"
	"github.com/opencontainers/runc/libcontainer/configs"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func AddDevicesForDump(next types.Dump) types.Dump {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (chan int, error) {
		container, ok := ctx.Value(runc_keys.CONTAINER_CONTEXT_KEY).(*libcontainer.Container)
		if !ok {
			return nil, status.Errorf(codes.FailedPrecondition, "failed to get container from context")
		}

		// TODO: return early if pre-dump, as we don't do all of this for pre-dump

		if req.GetCriu() == nil {
			req.Criu = &criu_proto.CriuOpts{}
		}

		config := container.Config()
		rootfs := config.Rootfs

		for _, d := range config.Devices {
			m := &configs.Mount{Destination: d.Path, Source: d.Path}
			filesystem.CriuAddExternalMount(req.Criu, m, rootfs)
		}

		return next(ctx, opts, resp, req)
	}
}

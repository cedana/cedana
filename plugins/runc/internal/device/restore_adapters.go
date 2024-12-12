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
	"google.golang.org/protobuf/proto"
)

func AddDevicesForRestore(next types.Restore) types.Restore {
	return func(ctx context.Context, server types.ServerOpts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (chan int, error) {
		container, ok := ctx.Value(runc_keys.CONTAINER_CONTEXT_KEY).(*libcontainer.Container)
		if !ok {
			return nil, status.Errorf(codes.FailedPrecondition, "failed to get container from context")
		}

		if req.GetCriu() == nil {
			req.Criu = &criu_proto.CriuOpts{}
		}

		config := container.Config()
		rootfs := config.Rootfs

		for _, node := range config.Devices {
			m := &configs.Mount{Destination: node.Path, Source: node.Path}
			filesystem.CriuAddExternalMount(req.Criu, m, rootfs)
		}

		return next(ctx, server, resp, req)
	}
}

func HandleEvasiveDevicesForRestore(next types.Restore) types.Restore {
	return func(ctx context.Context, server types.ServerOpts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (chan int, error) {
		if req.GetCriu() == nil {
			req.Criu = &criu_proto.CriuOpts{}
		}

		req.Criu.EvasiveDevices = proto.Bool(true)

		return next(ctx, server, resp, req)
	}
}

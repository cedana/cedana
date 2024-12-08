package device

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/pkg/criu"
	"github.com/cedana/cedana/pkg/types"
	runc_keys "github.com/cedana/cedana/plugins/runc/pkg/keys"
	"github.com/opencontainers/runc/libcontainer"
	"github.com/opencontainers/runc/libcontainer/cgroups"
	"github.com/opencontainers/runc/libcontainer/configs"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func AddDevicesForDump(next types.Dump) types.Dump {
	return func(ctx context.Context, server types.ServerOpts, nfy *criu.NotifyCallbackMulti, resp *daemon.DumpResp, req *daemon.DumpReq) (chan int, error) {
		container, ok := ctx.Value(runc_keys.CONTAINER_CONTEXT_KEY).(*libcontainer.Container)
		if !ok {
			return nil, status.Errorf(codes.FailedPrecondition, "failed to get container from context")
		}

		if req.GetCriu() == nil {
			req.Criu = &criu_proto.CriuOpts{}
		}

		// TODO: return early if pre-dump, as we don't do all of this for pre-dump

		config := container.Config()
		rootfs := config.Rootfs

		for _, m := range container.Config().Mounts {
			hasCgroupns := config.Namespaces.Contains(configs.NEWCGROUP)
			switch m.Device {
			case "bind":
				CriuAddExternalMount(req.Criu, m, rootfs)
			case "cgroup":
				if cgroups.IsCgroup2UnifiedMode() || hasCgroupns {
					// real mount(s)
					continue
				}
				// a set of "external" bind mounts
				binds, err := GetCgroupMounts(m)
				if err != nil {
					return nil, status.Errorf(
						codes.Internal,
						"failed to get cgroup mounts: %v",
						err,
					)
				}
				for _, b := range binds {
					CriuAddExternalMount(req.Criu, b, rootfs)
				}
			}
		}

		for _, d := range config.Devices {
			m := &configs.Mount{Destination: d.Path, Source: d.Path}
			CriuAddExternalMount(req.Criu, m, rootfs)
		}

		return next(ctx, server, nfy, resp, req)
	}
}

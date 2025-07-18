package filesystem

import (
	"context"
	"fmt"
	"os"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/pkg/types"
	runc_keys "github.com/cedana/cedana/plugins/runc/pkg/keys"
	"github.com/opencontainers/cgroups"
	"github.com/opencontainers/runc/libcontainer"
	"github.com/opencontainers/runc/libcontainer/configs"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const extDescriptorsFilename = "descriptors.json"

func AddMountsForDump(next types.Dump) types.Dump {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (code func() <-chan int, err error) {
		container, ok := ctx.Value(runc_keys.CONTAINER_CONTEXT_KEY).(*libcontainer.Container)
		if !ok {
			return nil, status.Errorf(codes.FailedPrecondition, "failed to get container from context")
		}

		// TODO: return early if pre-dump, as we don't do all of this for pre-dump

		if req.Criu == nil {
			req.Criu = &criu_proto.CriuOpts{}
		}

		config := container.Config()
		rootfs := config.Rootfs

		hasCgroupns := config.Namespaces.Contains(configs.NEWCGROUP)
		for _, m := range config.Mounts {
			switch m.Device {
			case "bind":
				dest := SecureJoin(rootfs, m.Destination)
				req.Criu.External = append(req.Criu.External, fmt.Sprintf("mnt[%s]:%s", dest, dest))
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
					dest := SecureJoin(rootfs, b.Destination)
					req.Criu.External = append(req.Criu.External, fmt.Sprintf("mnt[%s]:%s", dest, dest))
				}
			}
		}

		return next(ctx, opts, resp, req)
	}
}

func AddMaskedPathsForDump(next types.Dump) types.Dump {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (code func() <-chan int, err error) {
		container, ok := ctx.Value(runc_keys.CONTAINER_CONTEXT_KEY).(*libcontainer.Container)
		if !ok {
			return nil, status.Errorf(codes.FailedPrecondition, "failed to get container from context")
		}

		if req.Criu == nil {
			req.Criu = &criu_proto.CriuOpts{}
		}

		config := container.Config()
		state, err := container.State()
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to get container state: %v", err)
		}

		for _, path := range config.MaskPaths {
			fi, err := os.Stat(fmt.Sprintf("/proc/%d/root/%s", state.InitProcessPid, path))
			if err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return nil, status.Errorf(codes.Internal, "failed to stat %s: %v", path, err)
			}
			if fi.IsDir() {
				continue
			}

			req.Criu.External = append(req.Criu.External, fmt.Sprintf("mnt[%s]:%s", path, "/dev/null"))
		}

		return next(ctx, opts, resp, req)
	}
}

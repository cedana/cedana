package cgroup

import (
	"context"
	"fmt"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/pkg/types"
	runc_keys "github.com/cedana/cedana/plugins/runc/pkg/keys"
	"github.com/opencontainers/runc/libcontainer/cgroups"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

func ManageCgroupsForDump(mode criu_proto.CriuCgMode) types.Adapter[types.Dump] {
	return func(next types.Dump) types.Dump {
		return func(ctx context.Context, server types.ServerOpts, resp *daemon.DumpResp, req *daemon.DumpReq) (chan int, error) {
			if req.GetCriu() == nil {
				req.Criu = &criu_proto.CriuOpts{}
			}

			req.Criu.ManageCgroups = proto.Bool(true)
			req.Criu.ManageCgroupsMode = &mode

			return next(ctx, server, resp, req)
		}
	}
}

func UseCgroupFreezerIfAvailableForDump(next types.Dump) types.Dump {
	return func(ctx context.Context, server types.ServerOpts, resp *daemon.DumpResp, req *daemon.DumpReq) (chan int, error) {
		manager, ok := ctx.Value(runc_keys.CONTAINER_CGROUP_MANAGER_CONTEXT_KEY).(cgroups.Manager)
		if !ok {
			return nil, status.Errorf(codes.FailedPrecondition, "failed to get cgroup manager from context")
		}

		version, err := server.CRIU.GetCriuVersion(ctx)
		if err != nil {
			return nil, status.Error(codes.Internal, fmt.Sprintf("failed to get CRIU version: %v", err))
		}

		if req.GetCriu() == nil {
			req.Criu = &criu_proto.CriuOpts{}
		}

		if !cgroups.IsCgroup2UnifiedMode() || version >= 31400 {
			log.Debug().Msg("using cgroup freezer for dump")
			if fcg := manager.Path("freezer"); fcg != "" {
				req.Criu.FreezeCgroup = proto.String(fcg)
			}
		}

		return next(ctx, server, resp, req)
	}
}

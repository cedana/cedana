package cgroup

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/pkg/types"
	slurm_keys "github.com/cedana/cedana/plugins/slurm/pkg/keys"
	"github.com/opencontainers/cgroups"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

func UseCgroupFreezerIfAvailableForDump(next types.Dump) types.Dump {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (code func() <-chan int, err error) {
		manager, ok := ctx.Value(slurm_keys.CGROUP_MANAGER_CONTEXT_KEY).(cgroups.Manager)
		if !ok {
			return nil, status.Errorf(codes.FailedPrecondition, "failed to get cgroup manager from context")
		}

		version, err := opts.CRIU.GetCriuVersion(ctx)
		if err != nil {
			return nil, status.Error(codes.Internal, fmt.Sprintf("failed to get CRIU version: %v", err))
		}

		if req.GetCriu() == nil {
			req.Criu = &criu_proto.CriuOpts{}
		}

		if !cgroups.IsCgroup2UnifiedMode() || version >= 31400 {
			if fcg := manager.Path("freezer"); fcg != "" {
				var checkFile string
				if cgroups.IsCgroup2UnifiedMode() || strings.Contains(fcg, ":") {
					checkFile = filepath.Join(fcg, "cgroup.freeze")
				} else {
					checkFile = filepath.Join(fcg, "freezer.state")
				}
				if _, err := os.Stat(checkFile); err == nil {
					log.Debug().Str("path", fcg).Msg("using freezer cgroup path")
					req.Criu.FreezeCgroup = proto.String(fcg)
					return next(ctx, opts, resp, req)
				}
				log.Debug().Str("path", fcg).Str("file", checkFile).Msg("freezer cgroup path does not exist")
			}
		}

		return next(ctx, opts, resp, req)
	}
}

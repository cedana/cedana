package namespaces

import (
	"context"
	"fmt"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/pkg/types"
	"github.com/opencontainers/runc/libcontainer/configs"
	"github.com/rs/zerolog/log"
	"golang.org/x/sys/unix"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func IgnoreNamespacesForDump(nsTypes ...configs.NamespaceType) types.Adapter[types.Dump] {
	return func(next types.Dump) types.Dump {
		return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (code func() <-chan int, err error) {
			if req.Criu == nil {
				req.Criu = &criu_proto.CriuOpts{}
			}

			emptyNs := req.Criu.GetEmptyNs()

			for _, t := range nsTypes {
				ns := &configs.Namespace{Type: t}
				emptyNs |= uint32(ns.Syscall())
			}

			req.Criu.EmptyNs = &emptyNs

			return next(ctx, opts, resp, req)
		}
	}
}

func AddExternalNamespacesForDump(nsTypes ...configs.NamespaceType) types.Adapter[types.Dump] {
	return func(next types.Dump) types.Dump {
		return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (code func() <-chan int, err error) {
			version, err := opts.CRIU.GetCriuVersion(ctx)
			if err != nil {
				return nil, status.Error(codes.Internal, fmt.Sprintf("failed to get CRIU version: %v", err))
			}

			// Check CRIU compatibility for the namespace type

			for _, t := range nsTypes {
				switch t {
				case configs.NEWNET:
					minVersion := 31100
					if version < minVersion {
						log.Warn().
							Msgf("CRIU version is less than %d, skipping external network namespace handling", minVersion)
						return next(ctx, opts, resp, req)
					}
				case configs.NEWPID:
					minVersion := 31500
					if version < minVersion {
						log.Warn().
							Msgf("CRIU version is less than %d, skipping external pid namespace handling", minVersion)
						return next(ctx, opts, resp, req)
					}
				}

				// get the path of the namespace type
				// get nsBasepath from namespace.yaml next to slurm.conf
				nsPath := nsPathOf(t, req.Details.Slurm.PID)
				if nsPath == "" {
					// Nothing to do
					return next(ctx, opts, resp, req)
				}

				// CRIU expects the information about an external namespace
				// like this: --external <TYPE>[<inode>]:<key>
				// This <key> is always 'extRoot<TYPE>NS'.

				var ns unix.Stat_t
				if err := unix.Stat(nsPath, &ns); err != nil {
					return nil, status.Errorf(codes.Internal, "failed to stat %s: %v", nsPath, err)
				}
				external := fmt.Sprintf("%s[%d]:%s", configs.NsName(t), ns.Ino, CriuNsToKey(t))

				if req.Criu == nil {
					req.Criu = &criu_proto.CriuOpts{}
				}

				req.Criu.External = append(req.Criu.External, external)
			}

			return next(ctx, opts, resp, req)
		}
	}
}

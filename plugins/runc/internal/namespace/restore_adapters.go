package namespace

import (
	"context"
	"fmt"
	"os"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/pkg/keys"
	"github.com/cedana/cedana/pkg/types"
	runc_keys "github.com/cedana/cedana/plugins/runc/pkg/keys"
	"github.com/opencontainers/runc/libcontainer"
	"github.com/opencontainers/runc/libcontainer/configs"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

func IgnoreNamespacesForRestore(nsTypes ...configs.NamespaceType) types.Adapter[types.Restore] {
	return func(next types.Restore) types.Restore {
		return func(ctx context.Context, opts types.Opts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (chan int, error) {
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

// If the container is running in a network or PID namespace and has
// a path to the network or PID namespace configured, we will dump
// that network or PID namespace as an external namespace and we
// will expect that the namespace exists during restore.
// This basically means that CRIU will ignore the namespace
// and expect it to be setup correctly.
func InheritExternalNamespacesForRestore(nsTypes ...configs.NamespaceType) types.Adapter[types.Restore] {
	return func(next types.Restore) types.Restore {
		return func(ctx context.Context, opts types.Opts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (chan int, error) {
			container, ok := ctx.Value(runc_keys.CONTAINER_CONTEXT_KEY).(*libcontainer.Container)
			if !ok {
				return nil, status.Error(codes.FailedPrecondition, "failed to get container from context")
			}
			extraFiles, ok := ctx.Value(keys.EXTRA_FILES_CONTEXT_KEY).([]*os.File)
			if !ok {
				return nil, status.Error(codes.FailedPrecondition, "failed to get extra files from context")
			}

			config := container.Config()

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
							Msgf("CRIU version is less than %d, skipping external NEWNET namespace handling", minVersion)
						return next(ctx, opts, resp, req)
					}
				case configs.NEWPID:
					minVersion := 31500
					if version < minVersion {
						log.Warn().
							Msgf("CRIU version is less than %d, skipping external NEWPID namespace handling", minVersion)
						return next(ctx, opts, resp, req)
					}
				default:
					log.Warn().Msgf("inherit namespace should only be called for external NEWNET or NEWPID. Skipping.")
					return next(ctx, opts, resp, req)
				}

				if !config.Namespaces.Contains(t) {
					log.Debug().Msgf("container does not have %v namespace. Skipping.", t)
					return next(ctx, opts, resp, req)
				}

				nsPath := config.Namespaces.PathOf(t)
				if nsPath == "" {
					log.Debug().Msgf("container does not have external %v namespace path. Skipping.", t)
					return next(ctx, opts, resp, req)
				}

				// CRIU wants the information about an existing namespace
				// like this: --inherit-fd fd[<fd>]:<key>
				// The <key> needs to be the same as during checkpointing.
				// We are always using 'extRoot<TYPE>NS' as the key in this.

				nsFd, err := os.Open(nsPath)
				if err != nil {
					return nil, status.Errorf(codes.Internal, "external namespace file %s does not exist: %v", nsPath, err)
				}
				defer nsFd.Close()
				extraFiles = append(extraFiles, nsFd)

				criuOpts := req.GetCriu()
				if criuOpts == nil {
					criuOpts = &criu_proto.CriuOpts{}
				}

				criuOpts.InheritFd = append(criuOpts.InheritFd, &criu_proto.InheritFd{
					Key: proto.String(CriuNsToKey(t)),
					Fd:  proto.Int32(int32(2 + len(extraFiles))),
				})

				ctx = context.WithValue(ctx, keys.EXTRA_FILES_CONTEXT_KEY, extraFiles)
			}

			return next(ctx, opts, resp, req)
		}
	}
}

// For all other namespaces except NET and PID CRIU has
// a simpler way of joining the existing namespace if set
func JoinOtherExternalNamespacesForRestore(next types.Restore) types.Restore {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (chan int, error) {
		container, ok := ctx.Value(runc_keys.CONTAINER_CONTEXT_KEY).(*libcontainer.Container)
		if !ok {
			return nil, status.Error(codes.FailedPrecondition, "failed to get container from context")
		}

		config := container.Config()

		for _, ns := range config.Namespaces {
			switch ns.Type {
			case configs.NEWNET, configs.NEWPID:
				// Skip network and PID namespaces
				log.Debug().Msgf("skipping network and PID namespaces for joining as they should be inherited instead")
				continue
			default:
				nsPath := config.Namespaces.PathOf(ns.Type)
				if nsPath == "" {
					log.Debug().Msgf("container does not have %v namespace path. Skipping.", ns.Type)
					continue
				}
				// XXX: CRIU can't handle NEWCGROUP
				if ns.Type == configs.NEWCGROUP {
					return nil, status.Error(codes.FailedPrecondition, "CRIU does not support joining cgroup namespace")
				}
				// CRIU will issue a warning for NEWUSER:
				// criu/namespaces.c: 'join-ns with user-namespace is not fully tested and dangerous'
				criuOpts := req.GetCriu()
				if criuOpts == nil {
					criuOpts = &criu_proto.CriuOpts{}
				}
				criuOpts.JoinNs = append(criuOpts.JoinNs, &criu_proto.JoinNamespace{
					Ns:     proto.String(configs.NsName(ns.Type)),
					NsFile: proto.String(nsPath),
				})
			}
		}

		return next(ctx, opts, resp, req)
	}
}

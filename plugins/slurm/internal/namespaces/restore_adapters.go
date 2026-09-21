package namespaces

import (
	"context"
	"fmt"
	"os"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/pkg/types"
	"github.com/opencontainers/runc/libcontainer/configs"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

func IgnoreNamespacesForRestore(nsTypes ...configs.NamespaceType) types.Adapter[types.Restore] {
	return func(next types.Restore) types.Restore {
		return func(ctx context.Context, opts types.Opts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (code func() <-chan int, err error) {
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
		return func(ctx context.Context, opts types.Opts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (code func() <-chan int, err error) {
			version, err := opts.CRIU.GetCriuVersion(ctx)
			if err != nil {
				return nil, status.Error(codes.Internal, fmt.Sprintf("failed to get CRIU version: %v", err))
			}

			if req.Criu == nil {
				req.Criu = &criu_proto.CriuOpts{}
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

				nsPath := nsPathOf(t, req.Details.Slurm.PID)
				if nsPath == "" {
					log.Debug().Msgf("container does not have external %v namespace path. Skipping.", t)
					return next(ctx, opts, resp, req)
				}

				// CRIU wants the information about an existing namespace
				// like this: --inherit-fd fd[<fd>]:<key>
				// The <key> needs to be the same as during checkpointing.
				// We are always using 'extRoot<TYPE>NS' as the key in this.

				key := CriuNsToKey(t)
				fd := int32(3 + len(opts.ExtraFiles))

				if _, ok := opts.InheritFdMap[key]; ok {
					return nil, status.Errorf(codes.FailedPrecondition, "external namespace file %s already inherited", key)
				}
				opts.InheritFdMap[key] = fd

				nsFd, err := os.Open(nsPath)
				if err != nil {
					return nil, status.Errorf(codes.Internal, "external namespace file %s does not exist: %v", nsPath, err)
				}
				defer nsFd.Close()
				opts.ExtraFiles = append(opts.ExtraFiles, nsFd)
				req.Criu.InheritFd = append(req.Criu.InheritFd, &criu_proto.InheritFd{
					Key: proto.String(key),
					Fd:  proto.Int32(fd),
				})
			}

			return next(ctx, opts, resp, req)
		}
	}
}

// Counterpart of AddRecognizedExternalNamespacesForDump. Inherits exactly the
// external namespaces that were recorded in the dump, from the job being restored into.
// Does nothing if the dump has no external namespaces recorded.
func InheritRecognizedNamespacesForRestore(next types.Restore) types.Restore {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (code func() <-chan int, err error) {
		if opts.DumpFs == nil {
			log.Debug().Msg("dump filesystem is nil, skipping external namespace handling")
			return next(ctx, opts, resp, req)
		}

		nsTypes, err := loadExternalNamespaces(opts.DumpFs)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to load external namespaces from dump: %v", err)
		}
		if len(nsTypes) == 0 {
			return next(ctx, opts, resp, req)
		}

		version, err := opts.CRIU.GetCriuVersion(ctx)
		if err != nil {
			return nil, status.Error(codes.Internal, fmt.Sprintf("failed to get CRIU version: %v", err))
		}

		// When restoring from within the job, its namespaces are our own
		pid := req.GetDetails().GetSlurm().GetPID()
		if pid == 0 {
			pid = uint32(os.Getpid())
		}

		if req.Criu == nil {
			req.Criu = &criu_proto.CriuOpts{}
		}
		if opts.InheritFdMap == nil {
			opts.InheritFdMap = map[string]int32{}
		}

		for _, t := range nsTypes {
			// The dump has this namespace as external, so there's no restoring without it
			if ok, reason := criuSupportsExternal(t, version); !ok {
				return nil, status.Errorf(codes.FailedPrecondition, "dump has an external %s namespace: %s", configs.NsName(t), reason)
			}

			// CRIU wants the information about an existing namespace
			// like this: --inherit-fd fd[<fd>]:<key>
			// The <key> needs to be the same as during checkpointing.
			// We are always using 'extRoot<TYPE>NS' as the key in this.

			key := CriuNsToKey(t)
			fd := int32(3 + len(opts.ExtraFiles))

			if _, ok := opts.InheritFdMap[key]; ok {
				return nil, status.Errorf(codes.FailedPrecondition, "external namespace file %s already inherited", key)
			}
			opts.InheritFdMap[key] = fd

			nsPath := nsPathOf(t, pid)
			nsFd, err := os.Open(nsPath)
			if err != nil {
				return nil, status.Errorf(codes.Internal, "external namespace file %s does not exist: %v", nsPath, err)
			}
			defer nsFd.Close()

			opts.ExtraFiles = append(opts.ExtraFiles, nsFd)
			req.Criu.InheritFd = append(req.Criu.InheritFd, &criu_proto.InheritFd{
				Key: proto.String(key),
				Fd:  proto.Int32(fd),
			})
		}

		return next(ctx, opts, resp, req)
	}
}

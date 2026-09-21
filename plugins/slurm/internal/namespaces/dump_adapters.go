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

				var ns unix.Stat_t
				if err := unix.Stat(nsPath, &ns); err != nil {
					return nil, status.Errorf(codes.Internal, "failed to stat %s: %v", nsPath, err)
				}

				addExternalNamespace(req, t, uint64(ns.Ino))
			}

			return next(ctx, opts, resp, req)
		}
	}
}

// Detects the namespaces of the job that are external (created by whatever launched it,
// see RecognizeExternalNamespaces) and handles only those. Unlike AddExternalNamespacesForDump,
// this does not touch CRIU opts when the job is simply running in the host's namespaces.
//
//	net, pid -> left out of the dump using --external
//	mnt      -> CRIU is run inside it, as it has no notion of an external mount namespace
//
// What was done is recorded in the dump, for InheritRecognizedNamespacesForRestore.
func AddRecognizedExternalNamespacesForDump(next types.Dump) types.Dump {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (code func() <-chan int, err error) {
		pid := req.GetDetails().GetSlurm().GetPID()

		recognized, err := RecognizeExternalNamespaces(pid)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to recognize external namespaces: %v", err)
		}
		if len(recognized) == 0 {
			log.Debug().Uint32("PID", pid).Msg("no external namespaces recognized")
			return next(ctx, opts, resp, req)
		}

		version, err := opts.CRIU.GetCriuVersion(ctx)
		if err != nil {
			return nil, status.Error(codes.Internal, fmt.Sprintf("failed to get CRIU version: %v", err))
		}

		var handled []ExternalNamespace

		for _, ns := range recognized {
			name := configs.NsName(ns.Type)

			handling, reason := handlingFor(ns.Type, version)

			// Inside a mount namespace that comes with a PID namespace, /proc is that of the
			// PID namespace. CRIU would be looking for itself and the job in the wrong place.
			if handling == HandlingEnter && !inHostNamespace(configs.NEWPID, pid) {
				handling, reason = "", "job is not in the host's pid namespace"
			}

			log := log.With().Str("holder", string(ns.Holder)).Str("path", ns.Path).Uint64("inode", ns.Inode).Logger()

			switch handling {
			case HandlingExternal:
				log.Debug().Msgf("adding external %s namespace", name)
				addExternalNamespace(req, ns.Type, ns.Inode)

			case HandlingEnter:
				// CRIU opens some files by path, and so will plugins
				if dir := req.GetCriu().GetImagesDir(); dir != "" {
					visible, err := visibleInNamespace(pid, dir)
					if err != nil {
						return nil, status.Errorf(codes.Internal, "failed to check dump dir: %v", err)
					}
					if !visible {
						return nil, status.Errorf(codes.FailedPrecondition,
							"dump dir %s is not the same inside the job's %s namespace (held by %s %s), use a dir that is not private to the job",
							dir, name, ns.Holder, ns.Path)
					}
				}
				log.Debug().Msgf("running CRIU inside external %s namespace", name)
				opts.CRIU.SetMountNamespace(ns.Path)

			default:
				log.Warn().Msgf("%s, skipping external %s namespace handling", reason, name)
				continue
			}

			handled = append(handled, ExternalNamespace{Type: ns.Type, Handling: handling, Holder: ns.Holder})
		}

		if len(handled) > 0 {
			if opts.DumpFs == nil {
				return nil, status.Error(codes.FailedPrecondition, "dump filesystem is nil, cannot save external namespaces")
			}
			if err := saveExternalNamespaces(opts.DumpFs, handled); err != nil {
				return nil, status.Errorf(codes.Internal, "failed to save external namespaces to dump: %v", err)
			}
		}

		return next(ctx, opts, resp, req)
	}
}

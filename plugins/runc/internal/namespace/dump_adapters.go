package namespace

import (
	"context"
	"fmt"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/pkg/criu"
	"github.com/cedana/cedana/pkg/types"
	runc_keys "github.com/cedana/cedana/plugins/runc/pkg/keys"
	"github.com/opencontainers/runc/libcontainer"
	"github.com/opencontainers/runc/libcontainer/configs"
	"github.com/rs/zerolog/log"
	"golang.org/x/sys/unix"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// If the container is running in a network namespace and has
// a path to the network namespace configured, we will dump
// that network namespace as an external namespace and we
// will expect that the namespace exists during restore.
// This basically means that CRIU will ignore the namespace
// and expect to be setup correctly.
func AddExternalNamespacesForDump(t configs.NamespaceType) types.Adapter[types.Dump] {
	return func(next types.Dump) types.Dump {
		return func(ctx context.Context, server types.ServerOpts, nfy *criu.NotifyCallbackMulti, resp *daemon.DumpResp, req *daemon.DumpReq) (chan int, error) {
			container, ok := ctx.Value(runc_keys.CONTAINER_CONTEXT_KEY).(*libcontainer.Container)
			if !ok {
				return nil, status.Error(codes.FailedPrecondition, "failed to get container from context")
			}

			version, err := server.CRIU.GetCriuVersion(ctx)
			if err != nil {
				return nil, status.Error(codes.Internal, fmt.Sprintf("failed to get CRIU version: %v", err))
			}

			// Check CRIU compatibility for the namespace type

			switch t {
			case configs.NEWNET:
				minVersion := 31100
				if version < minVersion {
					log.Warn().
						Msgf("CRIU version is less than %d, skipping external network namespace handling", minVersion)
					return next(ctx, server, nfy, resp, req)
				}
			case configs.NEWPID:
				minVersion := 31500
				if version < minVersion {
					log.Warn().
						Msgf("CRIU version is less than %d, skipping external pid namespace handling", minVersion)
					return next(ctx, server, nfy, resp, req)
				}
			}

			config := container.Config()

			nsPath := config.Namespaces.PathOf(t)
			if nsPath == "" {
				// Nothing to do
				return next(ctx, server, nfy, resp, req)
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

			return next(ctx, server, nfy, resp, req)
		}
	}
}

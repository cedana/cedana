package network

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/pkg/criu"
	"github.com/cedana/cedana/pkg/types"
	runc_keys "github.com/cedana/cedana/plugins/runc/pkg/keys"
	"github.com/opencontainers/runc/libcontainer"
	"github.com/rs/zerolog/log"
	"golang.org/x/sys/unix"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

func UnlockNetworkAfterRestore(next types.Restore) types.Restore {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (code func() <-chan int, err error) {
		callback := &criu.NotifyCallback{
			NetworkUnlockFunc: func(ctx context.Context) error {
				// Not implemented, yet
				// see: libcontainer/criu_linux.go -> unlockNetwork
				log.Warn().Msg("not unlocking network - not implemented")
				return nil
			},
		}
		opts.CRIUCallback.Include(callback)

		return next(ctx, opts, resp, req)
	}
}

func RestoreNetworkConfiguration(next types.Restore) types.Restore {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (code func() <-chan int, err error) {
		container, ok := ctx.Value(runc_keys.CONTAINER_CONTEXT_KEY).(*libcontainer.Container)
		if !ok {
			return nil, status.Errorf(codes.FailedPrecondition, "failed to get container from context")
		}

		if req.Criu == nil {
			req.Criu = &criu_proto.CriuOpts{}
		}

		ignoredNamespaces := req.Criu.GetEmptyNs()

		if ignoredNamespaces&unix.CLONE_NEWNET != 0 {
			log.Debug().Msg("skipping network restore, marked in EmptyNs")
			return next(ctx, opts, resp, req)
		}

		config := container.Config()

		for _, iface := range config.Networks {
			switch iface.Type {
			case "veth":
				veth := new(criu_proto.CriuVethPair)
				veth.IfOut = proto.String(iface.HostInterfaceName)
				veth.IfIn = proto.String(iface.Name)
				req.Criu.Veths = append(req.Criu.Veths, veth)
			case "loopback":
				// Do nothing
			}
		}

		return next(ctx, opts, resp, req)
	}
}

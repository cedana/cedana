package network

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/types"
	"github.com/rs/zerolog/log"
)

func LockNetworkBeforeDump(next types.Dump) types.Dump {
	return func(ctx context.Context, server types.ServerOpts, resp *daemon.DumpResp, req *daemon.DumpReq) (chan int, error) {
		server.CRIUCallback.NetworkLockFunc = append(server.CRIUCallback.NetworkLockFunc, func(ctx context.Context) error {
			// Not implemented, yet
			// see: libcontainer/criu_linux.go -> lockNetwork
			log.Warn().Msg("not locking network - not implemented")
			return nil
		})

		return next(ctx, server, resp, req)
	}
}

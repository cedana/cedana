package network

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/criu"
	"github.com/cedana/cedana/pkg/types"
	"github.com/rs/zerolog/log"
)

func LockNetworkBeforeDump(next types.Dump) types.Dump {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (code func() <-chan int, err error) {
		callback := &criu.NotifyCallback{
			NetworkLockFunc: func(ctx context.Context) error {
				// Not implemented, yet
				// see: libcontainer/criu_linux.go -> lockNetwork
				log.Warn().Msg("not locking network - not implemented")
				return nil
			},
		}
		opts.CRIUCallback.Include(callback)

		return next(ctx, opts, resp, req)
	}
}

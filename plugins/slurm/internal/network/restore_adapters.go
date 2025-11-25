package network

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/criu"
	"github.com/cedana/cedana/pkg/types"
	"github.com/rs/zerolog/log"
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

package criu

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/barrier"
	"github.com/cedana/cedana/pkg/criu"
	"github.com/cedana/cedana/pkg/types"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func SeizeTogetherForDump(next types.Dump) types.Dump {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (code func() <-chan int, err error) {
		group, size, ok, err := barrier.FromGroup(ctx)
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		if !ok || size < 2 {
			return next(ctx, opts, resp, req)
		}

		member, err := barrier.Default.Join(group, size, barrier.DEFAULT_TIMEOUT)
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		defer func() { member.Leave(err) }()

		log := log.With().Str("group", group).Int("size", size).Logger()

		opts.CRIUCallback.Include(&criu.NotifyCallback{
			Name: "seize-barrier",
			// The first notification after CRIU has seized the tree, and before it dumps anything
			QueryExtFilesFunc: func(ctx context.Context) ([]string, error) {
				log.Debug().Msg("process tree seized, waiting for the rest of the seize group")
				if err := member.Arrive(ctx); err != nil {
					return nil, err
				}
				log.Debug().Msg("seize group complete, dumping")
				return nil, nil
			},
		})

		return next(ctx, opts, resp, req)
	}
}

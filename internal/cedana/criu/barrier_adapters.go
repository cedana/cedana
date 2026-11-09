package criu

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/pkg/barrier"
	"github.com/cedana/cedana/pkg/criu"
	"github.com/cedana/cedana/pkg/types"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// the kernel has access to setting JOBCTL_TRAP_FREEZE in the bit map task->jobctl while it
// iterates over the pids in the cgroup, we do the same thing but with ptrace seize which sets
// JOBCTL_TRAP_STOP to avoid SIGSTOP which uses JOBCTL_TRAP_PENDING which requires SIGCONT possibly
// trigger handlers in the app, which makes it not transparent. unfortunately we don’t have access
// to the task struct in userspace ofc so we are forced to go through the ptrace seize slow down,
// so yeah the kernel is faster for freezing multiple processes but we don’t have many options
// https://github.com/torvalds/linux/blob/v7.3/kernel/ptrace.c#L1277
// https://github.com/torvalds/linux/blob/v7.3/kernel/cgroup/freezer.c#L161
// https://github.com/torvalds/linux/blob/v7.3/kernel/signal.c#L2666
func SeizeTogetherForDump(next types.Dump) types.Dump {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (code func() <-chan int, err error) {
		group, size, ok, err := barrier.FromGroup(ctx)
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		if !ok || size < 2 {
			return next(ctx, opts, resp, req)
		}

		seized, err := barrier.Default.Join(group, size, barrier.DEFAULT_TIMEOUT)
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		defer func() { seized.Leave(err) }()

		dumped, err := barrier.Default.JoinStage(group, barrier.STAGE_DUMPED, size, barrier.DUMPED_TIMEOUT)
		if err != nil {
			seized.Leave(err)
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		defer func() { dumped.Leave(err) }()

		log := log.With().Str("group", group).Int("size", size).Logger()

		opts.CRIUCallback.Include(&criu.NotifyCallback{
			Name: "seize-barrier",
			// Seize notification
			QueryExtFilesFunc: func(ctx context.Context) ([]string, error) {
				log.Debug().Msg("process tree seized, waiting for the rest of the seize group")
				if err := seized.Arrive(ctx); err != nil {
					return nil, err
				}
				log.Debug().Msg("seize group complete, dumping")
				return nil, nil
			},
			// Dump completed notification
			PostDumpFunc: func(ctx context.Context, opts *criu_proto.CriuOpts) error {
				log.Debug().Msg("process tree dumped, waiting for the rest of the seize group")
				if err := dumped.Arrive(ctx); err != nil {
					return err
				}
				log.Debug().Msg("seize group dumped, resuming")
				return nil
			},
		})

		return next(ctx, opts, resp, req)
	}
}

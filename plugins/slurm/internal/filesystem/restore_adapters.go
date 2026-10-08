//go:build linux

package filesystem

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/plugins/slurm/internal/namespaces"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Counterpart of DumpPrivateMounts. Puts back the contents of the private mounts in the dump,
// into the same mounts of the job being restored into, for CRIU to find the job's files in.
// Does nothing if the dump has no private mounts.
func RestorePrivateMounts(next types.Restore) types.Restore {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (code func() <-chan int, err error) {
		if opts.DumpFs == nil {
			log.Debug().Msg("dump filesystem is nil, skipping private mounts")
			return next(ctx, opts, resp, req)
		}

		mounts, err := loadPrivateMounts(opts.DumpFs)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to load private mounts from dump: %v", err)
		}
		if len(mounts) == 0 {
			return next(ctx, opts, resp, req)
		}

		// Never our own for lack of one, as we (e.g. the daemon) may well be outside of the job
		pid := req.GetDetails().GetSlurm().GetPID()
		if pid == 0 {
			return nil, status.Errorf(codes.FailedPrecondition,
				"dump has private mounts, but no process of slurm job %d was found to restore them through", req.GetDetails().GetSlurm().GetJobID())
		}

		private, err := namespaces.SlurmMounts(pid, req.GetDetails().GetSlurm().GetJobID())
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to recognize the job's private mounts: %v", err)
		}
		destinations := map[string]namespaces.PrivateMount{}
		for _, m := range private {
			destinations[m.Mountpoint] = m
		}

		// Check all before touching any. If SLURM did not give the job being restored into a
		// directory of its own there, this node is set up differently and we'd be writing the
		// job's files into what is shared with the host.
		for _, m := range mounts {
			if _, ok := destinations[m.Mountpoint]; !ok {
				return nil, status.Errorf(codes.FailedPrecondition,
					"dump has the contents of the job's private %s, but SLURM gave the job being restored into no directory of its own there", m.Mountpoint)
			}
		}

		for _, m := range mounts {
			log.Debug().Str("mountpoint", m.Mountpoint).Str("archive", m.Archive).Msg("restoring private mount")

			archive, err := opts.DumpFs.Open(m.Archive)
			if err != nil {
				return nil, status.Errorf(codes.Internal, "failed to open %s from dump: %v", m.Archive, err)
			}
			err = extractDir(archive, pathInJob(pid, m.Mountpoint))
			archive.Close()
			if err != nil {
				return nil, status.Errorf(codes.Internal, "failed to restore private mount %s: %v", m.Mountpoint, err)
			}
		}

		return next(ctx, opts, resp, req)
	}
}

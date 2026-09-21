//go:build linux

package filesystem

import (
	"context"
	"os"

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

		// When restoring from within the job, its mounts are our own
		pid := req.GetDetails().GetSlurm().GetPID()
		if pid == 0 {
			pid = uint32(os.Getpid())
		}

		private, err := namespaces.RecognizePrivateMounts(pid)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to recognize private mounts: %v", err)
		}
		fstypes := map[string]string{}
		for _, m := range private {
			fstypes[m.Mountpoint] = m.FSType
		}

		// Check all before touching any. If the job being restored into does not have the mount
		// to itself, this node is set up differently and we'd be writing the job's files into
		// what is shared with the host (e.g. its /var/tmp).
		for _, m := range mounts {
			fstype, ok := fstypes[m.Mountpoint]
			if !ok {
				return nil, status.Errorf(codes.FailedPrecondition,
					"dump has the contents of a private %s on %s, but the job being restored into does not have a private mount there", m.FSType, m.Mountpoint)
			}
			if fstype != m.FSType {
				return nil, status.Errorf(codes.FailedPrecondition,
					"dump has the contents of a private %s on %s, but the job being restored into has a %s there", m.FSType, m.Mountpoint, fstype)
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

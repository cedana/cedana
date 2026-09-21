//go:build linux

package filesystem

import (
	"context"
	"fmt"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	criu_client "github.com/cedana/cedana/pkg/criu"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/plugins/slurm/internal/namespaces"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// When CRIU runs inside the job's external mount namespace (see namespaces.AddRecognizedExternalNamespacesForDump),
// it dumps files by path and knows nothing of the mounts. A tmpfs that exists only in that
// namespace (e.g. a PAM module giving each session its own /var/tmp) will be a new, empty one
// in the job restored into. So its contents go into the dump.
//
// Does nothing otherwise. If CRIU is dumping the mount namespace, it dumps the tmpfs too.
func DumpPrivateMounts(next types.Dump) types.Dump {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (code func() <-chan int, err error) {
		if opts.CRIU.MountNamespace() == "" {
			return next(ctx, opts, resp, req)
		}

		pid := req.GetDetails().GetSlurm().GetPID()

		private, err := namespaces.RecognizePrivateMounts(pid)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to recognize private mounts: %v", err)
		}

		var mounts []privateMount
		var total uint64

		for _, m := range private {
			if m.FSType != TMPFS {
				// Backed by something that outlives the namespace. Where it is on
				// the node restored on, is for whatever mounts it there to decide.
				log.Debug().Str("mountpoint", m.Mountpoint).Str("fstype", m.FSType).Msg("not dumping private mount that is not a tmpfs")
				continue
			}

			used, err := usedBytes(pathInJob(pid, m.Mountpoint))
			if err != nil {
				return nil, status.Errorf(codes.Internal, "failed to get size of private mount %s: %v", m.Mountpoint, err)
			}
			total += used

			mounts = append(mounts, privateMount{
				Mountpoint: m.Mountpoint,
				FSType:     m.FSType,
				Archive:    fmt.Sprintf("%s-%d.tar", PRIVATE_MOUNTS_PREFIX, len(mounts)),
			})
		}

		if len(mounts) == 0 {
			return next(ctx, opts, resp, req)
		}

		if max := maxPrivateMountsSize(); total > max {
			return nil, status.Errorf(codes.FailedPrecondition,
				"private mounts of the job hold %d bytes, more than the %d allowed in a dump (set %s to change)", total, max, PRIVATE_MOUNTS_MAX_SIZE_ENV)
		}

		if opts.DumpFs == nil {
			return nil, status.Error(codes.FailedPrecondition, "dump filesystem is nil, cannot save private mounts")
		}
		if err := savePrivateMounts(opts.DumpFs, mounts); err != nil {
			return nil, status.Errorf(codes.Internal, "failed to save private mounts to dump: %v", err)
		}

		dumpFs := opts.DumpFs

		// Post-dump the job is still frozen, so what's archived is what the images hold.
		// Callbacks run in reverse order of registration, so this is before any compression/upload of the dump.
		opts.CRIUCallback.Include(&criu_client.NotifyCallback{
			PostDumpFunc: func(ctx context.Context, _ *criu_proto.CriuOpts) error {
				for _, m := range mounts {
					log.Debug().Str("mountpoint", m.Mountpoint).Str("archive", m.Archive).Msg("dumping private mount")
					if err := archiveToDump(dumpFs, m.Archive, pathInJob(pid, m.Mountpoint)); err != nil {
						return fmt.Errorf("failed to dump private mount %s: %w", m.Mountpoint, err)
					}
				}
				return nil
			},
		})

		return next(ctx, opts, resp, req)
	}
}

func archiveToDump(dumpFs afero.Fs, name, dir string) (err error) {
	file, err := dumpFs.Create(name)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := file.Close(); err == nil {
			err = cerr
		}
	}()

	return archiveDir(dir, file)
}

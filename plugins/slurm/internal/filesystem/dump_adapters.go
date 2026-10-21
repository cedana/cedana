//go:build linux

package filesystem

import (
	"context"
	"fmt"
	"strings"

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

// The GPU controller of the job keeps its shared memory in the job's /dev/shm: the segment
// of gpu.shm_size, cedana-gpu.<id>, with its .booking file and its .misc directory, and its
// logs under gpu.log_dir, /tmp by default, as cedana-gpu.<id>/. The GPU plugin makes them anew
// for the restored job, and the segment alone, 24 GiB on a serving node, is more than a dump
// may hold. So the controller's entries are left out of every private mount.
const gpuControllerPrefix = "cedana-gpu."

// isGPUControllerEntry tells an entry at the top of a private mount that is the GPU controller's
func isGPUControllerEntry(rel string) bool {
	return !strings.Contains(rel, "/") && strings.HasPrefix(rel, gpuControllerPrefix)
}

// A job in a mount namespace of its own is dumped from inside it (see
// namespaces.AddRecognizedExternalNamespacesForDump), so CRIU dumps files by path and knows
// nothing of the mounts. The directories SLURM's namespace plugin gives the job (e.g. its /tmp)
// are made for it and removed with it, and the job restored into gets new, empty ones. So
// their contents go into the dump.
//
// Does nothing for a job without such directories.
func DumpPrivateMounts(next types.Dump) types.Dump {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (code func() <-chan int, err error) {
		details := req.GetDetails().GetSlurm()
		pid := details.GetPID()

		private, err := namespaces.SlurmMounts(pid, details.GetJobID())
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to recognize the job's private mounts: %v", err)
		}

		var mounts []privateMount
		var total uint64

		for _, m := range private {
			log.Debug().Str("mountpoint", m.Mountpoint).Str("root", m.Root).Msg("private mount of the job")

			size, err := contentSize(pathInJob(pid, m.Mountpoint), isGPUControllerEntry)
			if err != nil {
				return nil, status.Errorf(codes.Internal, "failed to get size of private mount %s: %v", m.Mountpoint, err)
			}
			total += size

			mounts = append(mounts, privateMount{
				Mountpoint: m.Mountpoint,
				FSType:     m.FSType,
				Archive:    fmt.Sprintf("%s-%d.tar", PRIVATE_MOUNTS_PREFIX, len(mounts)),
			})
		}

		if len(mounts) == 0 {
			return next(ctx, opts, resp, req)
		}

		max := maxPrivateMountsSize()
		if total > max {
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
				left := max // the job was still running when checked above
				for _, m := range mounts {
					log.Debug().Str("mountpoint", m.Mountpoint).Str("archive", m.Archive).Msg("dumping private mount")
					written, err := archiveToDump(dumpFs, m.Archive, pathInJob(pid, m.Mountpoint), left, isGPUControllerEntry)
					if err != nil {
						return fmt.Errorf("failed to dump private mount %s: %w", m.Mountpoint, err)
					}
					left -= written
				}
				return nil
			},
		})

		return next(ctx, opts, resp, req)
	}
}

func archiveToDump(dumpFs afero.Fs, name, dir string, max uint64, skip skipFunc) (written uint64, err error) {
	file, err := dumpFs.Create(name)
	if err != nil {
		return 0, err
	}
	defer func() {
		if cerr := file.Close(); err == nil {
			err = cerr
		}
	}()

	return archiveDir(dir, file, max, skip)
}

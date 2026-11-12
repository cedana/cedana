//go:build linux

package job

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"

	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/rs/zerolog/log"

	slurm_keys "github.com/cedana/cedana/plugins/slurm/pkg/keys"
	slurm_utils "github.com/cedana/cedana/plugins/slurm/pkg/utils"

	"github.com/opencontainers/cgroups"
	cgroupsManager "github.com/opencontainers/cgroups/manager"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

func GetSlurmJobForRestore(next types.Restore) types.Restore {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (code func() <-chan int, err error) {
		details := req.GetDetails().GetSlurm()
		jid := details.GetJobID()
		pid := details.GetPID()
		self := uint32(os.Getpid())
		if pid == 0 {
			if selfInJobCgroup(self, jid) {
				pid = self
			}
		} else if path, err := cgroupPathFromProc(pid); err == nil && !inJobCgroup(path, jid) {
			// Its namespaces and mounts are what the job gets restored into
			return nil, status.Errorf(codes.FailedPrecondition, "PID %d is not of slurm job %d, it's in cgroup %s", pid, jid, path)
		}

		path, err := ResolveJobCgroupPath(jid, pid)
		if err != nil {
			return nil, err
		}

		config := &cgroups.Cgroup{
			Path:      path,
			Resources: &cgroups.Resources{},
		}
		manager, err := cgroupsManager.New(config)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to load cgroup2 for slurm job %d: %v", jid, err)
		}
		ctx = context.WithValue(ctx, slurm_keys.CGROUP_MANAGER_CONTEXT_KEY, manager)

		// Check that cgroup is not frozen. Do not use Exists() here
		// since in cgroup v1 it only checks "devices" controller.
		st, err := manager.GetFreezerState()
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to get cgroup freezer state: %v", err)
		}
		if st == cgroups.Frozen {
			return nil, status.Errorf(codes.FailedPrecondition, "container's cgroup unexpectedly frozen")
		}

		// We may be outside of the job (e.g. the daemon), whose processes are then the only way to
		// its namespaces and mounts. Not having one is for whatever needs it to complain about.
		if pid == 0 {
			pids, err := manager.GetAllPids()
			if err != nil {
				log.Warn().Err(err).Uint32("job_id", jid).Msg("failed to get the processes of the job")
			}
			pid = pickJobPID(pids, self, processExists)
		}
		if pid != 0 {
			log.Debug().Uint32("job_id", jid).Uint32("pid", pid).Msg("using process of the job for its namespaces and mounts")
			details.PID = pid
		}

		return next(ctx, opts, resp, req)
	}
}

// We manually restore the SLURM script because SLURM
// will delete the script used to launch the job step
func RestoreSlurmScript(next types.Restore) types.Restore {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (code func() <-chan int, err error) {
		// Slurm's spooled script, from the saved copy in memory. The memfd stays open until CRIU
		// has restored.
		memfd, err := inheritSpoolScript(&opts, req)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to restore the slurm script: %v", err)
		}
		if memfd != nil {
			defer memfd.Close()
		}

		state := resp.GetState()
		if state == nil {
			log.Warn().Msg("no process info found. it should have been filled by an adapter")
			return next(ctx, opts, resp, req)
		}

		// Where the job being restored into has it, which may be its own /tmp rather than ours
		pid := req.GetDetails().GetSlurm().GetPID()

		utils.WalkTree(state, "OpenFiles", "Children", func(f *daemon.File) bool {
			if path := f.GetPath(); isJobScript(path) {
				contents, err := slurm_utils.LoadScriptFromDump(SLURM_SCRIPT_FILE, opts.DumpFs)
				if err != nil {
					log.Warn().Err(err).Msgf("failed to load slurm script from dump %s", path)
					return false
				}

				path = inRootOf(pid, path)
				err = os.MkdirAll(filepath.Dir(path), 0o755)
				if err != nil {
					log.Warn().Err(err).Msgf("failed to create directory for slurm script %s", path)
					return false
				}

				err = os.WriteFile(path, contents, 0o600)
				if err != nil {
					log.Warn().Err(err).Msgf("failed to restore slurm script file %s", path)
					return false
				}

				// As it was, or CRIU refuses it: "has bad mode"
				attrs := loadScriptAttrs(opts.DumpFs)
				if err := os.Chown(path, int(attrs.Uid), int(attrs.Gid)); err != nil {
					log.Warn().Err(err).Msgf("failed to set the owner of slurm script %s", path)
				}
				if err := os.Chmod(path, os.FileMode(attrs.Mode)); err != nil {
					log.Warn().Err(err).Msgf("failed to set the mode of slurm script %s", path)
				}

				return false
			}

			return true
		})

		return next(ctx, opts, resp, req)
	}
}

// inheritSpoolScript gives the restored shell the saved spooled script, from a sealed, read-only
// memfd at its recorded offset, through CRIU's --inherit-fd under the script's path. Nothing is
// written to disk or into Slurm's spool. A dump without a record restores as before.
func inheritSpoolScript(opts *types.Opts, req *daemon.RestoreReq) (*os.File, error) {
	record, err := loadSpoolScriptRecord(opts.DumpFs)
	if err != nil || record == nil {
		return nil, err
	}
	contents, err := slurm_utils.LoadScriptFromDump(SPOOL_SCRIPT_FILE, opts.DumpFs)
	if err != nil {
		return nil, err
	}
	memfd, err := scriptMemfd(contents, record.Offset)
	if err != nil {
		return nil, err
	}

	key := strings.TrimPrefix(record.Path, "/")
	if opts.InheritFdMap == nil {
		opts.InheritFdMap = map[string]int32{}
	}
	if _, ok := opts.InheritFdMap[key]; ok {
		memfd.Close()
		return nil, fmt.Errorf("file %s already inherited", key)
	}
	fd := int32(3 + len(opts.ExtraFiles))
	opts.InheritFdMap[key] = fd
	opts.ExtraFiles = append(opts.ExtraFiles, memfd)
	req.Criu.InheritFd = append(req.Criu.InheritFd, &criu_proto.InheritFd{
		Key: proto.String(key),
		Fd:  proto.Int32(fd),
	})
	return memfd, nil
}

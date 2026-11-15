//go:build linux

package job

import (
	"context"
	"fmt"
	"os"
	"strings"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"

	"github.com/cedana/cedana/pkg/types"
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

		return next(ctx, opts, resp, req)
	}
}

// inheritSpoolScript gives the restored shell the saved spooled script, from a sealed, read-only
// memfd at its recorded offset, through CRIU's --inherit-fd under the script's path, in place of
// the file Slurm deleted. Nothing is written to disk or into Slurm's spool. A dump without a
// record reopens the spool file, which works for the same job only.
//
// --inherit-fd supplies one handle for the path, so every process that had the script open
// shares it and its position. That is right for the usual single reader, forked subshells
// included. A script that opens itself again while it runs (bash "$0", source "$0") is not
// restored correctly: its second reader resumes at the shell's position.
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

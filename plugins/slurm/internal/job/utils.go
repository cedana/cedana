package job

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	cgroupRetryAttempts = 10
	cgroupRetryInterval = 500 * time.Millisecond
)

func ResolveJobCgroupPath(jid uint32, pid uint32) (string, error) {
	if pid > 0 {
		if path, err := cgroupPathFromProc(pid); err == nil {
			log.Debug().Str("path", path).Uint32("job_id", jid).Uint32("pid", pid).Msg("found cgroup path (from /proc)")
			return path, nil
		} else {
			log.Debug().Err(err).Uint32("job_id", jid).Uint32("pid", pid).Msg("could not resolve cgroup from /proc, falling back to job-scoped lookup")
		}
	}
	if path, err := getJobCgroupPathV2(jid); err == nil {
		return path, nil
	}
	return getJobCgroupPathV1(jid)
}

func getJobCgroupPathV2(jid uint32) (string, error) {
	const root = "/sys/fs/cgroup"
	leaf := fmt.Sprintf("job_%d/step_batch/user/task_special", jid)
	patterns := []string{
		fmt.Sprintf("%s/system.slice/*slurmstepd*.scope/%s", root, leaf),
		fmt.Sprintf("%s/system.slice/*.scope/system.slice/*slurmstepd*.scope/%s", root, leaf),
	}

	for attempt := range cgroupRetryAttempts {
		for _, pattern := range patterns {
			if matches, _ := filepath.Glob(pattern); len(matches) > 0 {
				path := matches[0][len(root):]
				log.Debug().Str("path", path).Uint32("job_id", jid).Int("attempt", attempt).Msg("found cgroup path (v2 by job id)")
				return path, nil
			}
		}
		if attempt < cgroupRetryAttempts-1 {
			time.Sleep(cgroupRetryInterval)
		}
	}

	return "", status.Errorf(codes.NotFound, "cgroup v2 path for slurm job %d not found", jid)
}

func selfInJobCgroup(pid, jid uint32) bool {
	path, err := cgroupPathFromProc(pid)
	if err != nil {
		return false
	}
	return strings.Contains(path, fmt.Sprintf("/job_%d/", jid))
}

func cgroupPathFromProc(pid uint32) (string, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/cgroup", pid))
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		path, ok := strings.CutPrefix(line, "0::")
		if !ok {
			continue
		}
		if path == "" || path == "/" {
			return "", fmt.Errorf("process %d is in the root cgroup", pid)
		}
		return path, nil
	}
	return "", fmt.Errorf("no cgroup v2 entry for process %d", pid)
}

func getJobCgroupPathV1(jid uint32) (string, error) {
	v1Pattern := fmt.Sprintf("/sys/fs/cgroup/freezer/slurm/uid_*/job_%d/step_batch", jid)

	for attempt := range cgroupRetryAttempts {
		matches, err := filepath.Glob(v1Pattern)
		if err != nil {
			return "", status.Errorf(codes.Internal, "failed to glob cgroup paths for slurm job %d with pattern %s: %v", jid, v1Pattern, err)
		}
		if len(matches) > 0 {
			path := matches[0][len("/sys/fs/cgroup"):]
			log.Debug().Str("path", path).Uint32("job_id", jid).Int("attempt", attempt).Msg("found cgroup path (v1)")
			return path, nil
		}

		if attempt < cgroupRetryAttempts-1 {
			log.Debug().Uint32("job_id", jid).Int("attempt", attempt).Msg("cgroup path not found, retrying")
			time.Sleep(cgroupRetryInterval)
		}
	}

	return "", status.Errorf(codes.NotFound, "cgroup path for slurm job %d does not exist after %d attempts", jid, cgroupRetryAttempts)
}

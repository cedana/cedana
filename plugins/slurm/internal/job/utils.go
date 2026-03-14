package job

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// getCgroupPathFromProc reads the cgroup path for a given PID from /proc/<pid>/cgroup.
// On cgroup v2, this returns a single line like "0::/path/to/cgroup".
// On cgroup v1, it returns multiple lines like "12:freezer:/slurm/uid_0/job_1/step_batch".
func getCgroupPathFromProc(pid uint32) (string, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/cgroup", pid))
	if err != nil {
		return "", err
	}

	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		parts := strings.SplitN(line, ":", 3)
		if len(parts) != 3 {
			continue
		}
		if parts[0] == "0" && parts[1] == "" {
			path := parts[2]
			if path != "" && path != "/" {
				return path, nil
			}
		}
	}

	return "", fmt.Errorf("no cgroup v2 path found in /proc/%d/cgroup", pid)
}

func GetJobCgroupPath(hostname string, jid uint32, pid uint32) (string, error) {
	// Try to read the cgroup path directly from procfs if PID is available.
	if pid > 0 {
		path, err := getCgroupPathFromProc(pid)
		if err == nil {
			if _, statErr := os.Stat("/sys/fs/cgroup" + path); statErr == nil {
				log.Debug().Uint32("pid", pid).Str("path", path).Msg("resolved cgroup path from /proc")
				return path, nil
			}
			log.Warn().Uint32("pid", pid).Str("path", path).Msg("cgroup path from /proc does not exist on filesystem")
		} else {
			log.Debug().Uint32("pid", pid).Err(err).Msg("failed to read cgroup path from /proc, falling back to well-known paths")
		}
	}

	// Fallback: check well-known cgroup v2 paths
	paths := []string{
		fmt.Sprintf("/system.slice/%s_slurmstepd.scope/job_%d/step_batch/user/task_special", hostname, jid),
		fmt.Sprintf("/system.slice/slurmstepd.scope/job_%d/step_batch/user/task_special", jid),
	}
	var path string
	for _, p := range paths {
		if _, err := os.Stat("/sys/fs/cgroup" + p); err == nil {
			path = p
			break
		}
	}

	// Fallback: check cgroup v1 freezer paths
	pattern := fmt.Sprintf("/sys/fs/cgroup/freezer/slurm/uid_*/job_%d/step_batch", jid)
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return "", status.Errorf(codes.Internal, "failed to glob cgroup paths for slurm job %d with pattern %s: %v", jid, pattern, err)
	}
	if matches != nil && len(matches) > 0 {
		path = matches[0][len("/sys/fs/cgroup"):]
	}
	if path == "" {
		return "", status.Errorf(codes.NotFound, "cgroup path for slurm job %d does not exist: %s", jid, path)
	}

	return path, nil
}

package job

import (
	"fmt"
	"os"
	"path/filepath"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func GetJobCgroupPath(hostname string, jid uint32) (string, error) {
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
	pattern := fmt.Sprintf("/sys/fs/cgroup/freezer/slurm/uid_*/job_%d/step_batch", jid)
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return "", status.Errorf(codes.Internal, "failed to glob cgroup paths for slurm job %d with pattern %s: %v", jid, pattern, err)
	}
	if len(matches) > 0 {
		path = matches[0][len("/sys/fs/cgroup/freezer"):]
	}
	if path == "" {
		return "", status.Errorf(codes.NotFound, "cgroup path for slurm job %d does not exist: %s", jid, path)
	}

	return path, nil
}

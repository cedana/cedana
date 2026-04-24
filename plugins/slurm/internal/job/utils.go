package job

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func cgroupPathExists(path string) bool {
	if path == "" {
		return false
	}

	_, err := os.Stat("/sys/fs/cgroup" + path)
	return err == nil
}

func getCurrentProcessCgroupPath(jid uint32) string {
	data, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		return ""
	}

	jobComponent := fmt.Sprintf("job_%d", jid)
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		parts := strings.Split(line, ":")
		if len(parts) != 3 {
			continue
		}

		path := strings.TrimSpace(parts[2])
		if !strings.Contains(path, jobComponent) {
			continue
		}
		if cgroupPathExists(path) {
			return path
		}
	}

	return ""
}

func GetJobCgroupPath(hostname string, jid uint32) (string, error) {
	if path := getCurrentProcessCgroupPath(jid); path != "" {
		return path, nil
	}

	paths := []string{
		fmt.Sprintf("/system.slice/%s_slurmstepd.scope/job_%d/step_batch", hostname, jid),
		fmt.Sprintf("/system.slice/%s_slurmstepd.scope/job_%d/step_batch/user/task_special", hostname, jid),
		fmt.Sprintf("/system.slice/slurmstepd.scope/job_%d/step_batch", jid),
		fmt.Sprintf("/system.slice/slurmstepd.scope/job_%d/step_batch/user/task_special", jid),
	}
	var path string
	for _, p := range paths {
		if cgroupPathExists(p) {
			path = p
			break
		}
	}
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

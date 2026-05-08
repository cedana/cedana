package job

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	cgroupRetryAttempts = 10
	cgroupRetryInterval = 500 * time.Millisecond
)

func GetJobCgroupPath(hostname string, jid uint32) (string, error) {
	paths := []string{
		fmt.Sprintf("/system.slice/%s_slurmstepd.scope/job_%d/step_batch/user/task_special", hostname, jid),
		fmt.Sprintf("/system.slice/slurmstepd.scope/job_%d/step_batch/user/task_special", jid),
	}
	v1Pattern := fmt.Sprintf("/sys/fs/cgroup/freezer/slurm/uid_*/job_%d/step_batch", jid)

	for attempt := range cgroupRetryAttempts {
		for _, p := range paths {
			if _, err := os.Stat("/sys/fs/cgroup" + p); err == nil {
				log.Debug().Str("path", p).Uint32("job_id", jid).Int("attempt", attempt).Msg("found cgroup path")
				return p, nil
			}
		}

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

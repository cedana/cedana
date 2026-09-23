package job

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
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
			if inJobCgroup(path, jid) {
				log.Debug().Str("path", path).Uint32("job_id", jid).Uint32("pid", pid).Msg("found cgroup path (from /proc)")
				return path, nil
			}
			log.Debug().Str("path", path).Uint32("job_id", jid).Uint32("pid", pid).Msg("cgroup path from /proc does not belong to job, falling back to job-scoped lookup")
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
	return inJobCgroup(path, jid)
}

// inJobCgroup reports whether path, a cgroup v2 path of a process, is inside the cgroup of job jid.
//
// Below the slurmstepd scope, SLURM names the job's cgroup job_<jid>. From 26.05 it uses the job's
// SLUID instead (e.g. sFNDM35NQ39R00), unless CgroupJobIdPaths=yes is set in cgroup.conf
// (https://slurm.schedmd.com/cgroup_v2.html). A SLUID does not contain the job ID, so a SLUID named
// cgroup is matched to its job through the job's slurmstepd.
func inJobCgroup(path string, jid uint32) bool {
	if strings.Contains(path, fmt.Sprintf("/job_%d/", jid)) {
		return true
	}
	jobDir, ok := jobCgroupDir(path)
	if !ok || !isSLUID(filepath.Base(jobDir)) {
		return false
	}
	owner, ok := jobCgroupJobID(jobDir)
	return ok && owner == jid
}

// jobCgroupDir returns the job's cgroup in path: path up to the component below the slurmstepd
// scope, which is slurmstepd.scope or <nodename>_slurmstepd.scope.
func jobCgroupDir(path string) (string, bool) {
	parts := strings.Split(path, "/")
	for i := len(parts) - 2; i >= 0; i-- {
		if strings.Contains(parts[i], "slurmstepd") && strings.HasSuffix(parts[i], ".scope") {
			return strings.Join(parts[:i+2], "/"), true
		}
	}
	return "", false
}

// jobCgroupJobID returns the job ID of jobDir, a job's cgroup relative to the cgroup root. Each
// step's slurmstepd runs in <jobDir>/step_<id>/slurm and is titled "slurmstepd: [<jid>.<step>]"
// by SLURM. Reading the title requires /proc to be mounted without hidepid.
func jobCgroupJobID(jobDir string) (uint32, bool) {
	procsFiles, _ := filepath.Glob(filepath.Join("/sys/fs/cgroup", jobDir, "step_*", "slurm", "cgroup.procs"))
	for _, procsFile := range procsFiles {
		procs, err := os.ReadFile(procsFile)
		if err != nil {
			continue
		}
		for _, pid := range strings.Fields(string(procs)) {
			cmdline, err := os.ReadFile(filepath.Join("/proc", pid, "cmdline"))
			if err != nil {
				continue
			}
			if jid, ok := parseSlurmstepdJobID(string(cmdline)); ok {
				return jid, true
			}
		}
	}
	return 0, false
}

// parseSlurmstepdJobID parses the job ID from a slurmstepd process title, "slurmstepd: [<jid>.<step>]".
func parseSlurmstepdJobID(cmdline string) (uint32, bool) {
	rest, ok := strings.CutPrefix(strings.TrimRight(cmdline, "\x00 "), "slurmstepd: [")
	if !ok {
		return 0, false
	}
	id, _, ok := strings.Cut(rest, ".")
	if !ok {
		return 0, false
	}
	jid, err := strconv.ParseUint(id, 10, 32)
	if err != nil {
		return 0, false
	}
	return uint32(jid), true
}

// isSLUID reports whether name is a SLUID as SLURM prints it: "s" followed by 13 characters of
// Crockford's base32 (print_sluid in
// https://github.com/SchedMD/slurm/blob/slurm-26-05-4-1/src/common/sluid.c).
func isSLUID(name string) bool {
	const base32 = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	if len(name) != 14 || name[0] != 's' {
		return false
	}
	for _, c := range name[1:] {
		if !strings.ContainsRune(base32, c) {
			return false
		}
	}
	return true
}

func cgroupPathFromProc(pid uint32) (string, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/cgroup", pid))
	if err != nil {
		return "", err
	}
	for line := range strings.SplitSeq(strings.TrimSpace(string(data)), "\n") {
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
	v1Pattern := fmt.Sprintf("/sys/fs/cgroup/freezer/slurm*/uid_*/job_%d/step_batch", jid)

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

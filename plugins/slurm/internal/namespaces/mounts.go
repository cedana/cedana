package namespaces

// Recognizes the directories SLURM's namespace plugin gives the job.
//
// job_container/tmpfs (namespace/tmpfs from 26.05) makes each directory it is
// configured with (Dirs, /tmp by default) under <basepath>/<jobid>/.<jobid>/ and
// bind-mounts it into the job's mount namespace. Made for the job and removed
// with it, so a job restored gets new, empty ones: what was in them goes into the
// dump. Its /dev/shm is a tmpfs of the kernel's, whose contents are the shared
// memory of the processes that map them, which CRIU dumps with the processes.

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

type mount struct {
	ID         int
	Device     string // major:minor. Unique to each instance of a virtual filesystem like tmpfs
	Root       string
	Mountpoint string
	FSType     string
}

// PrivateMount is a directory of the job's own, mounted into its namespace by SLURM
type PrivateMount struct {
	Mountpoint string
	FSType     string
	// Where it is on the node: <basepath>/<jobid>/.<jobid>/<dir>
	Root string
}

// SlurmMounts returns the mounts of pid that SLURM's namespace plugin made for the job,
// in the order they are mounted.
func SlurmMounts(pid uint32, jobID uint32) ([]PrivateMount, error) {
	jobPath := fmt.Sprintf("/proc/%d/mountinfo", pid)
	job, err := readMountinfo(jobPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", jobPath, err)
	}
	return slurmMounts(job, jobID), nil
}

// slurmMounts picks the mounts whose root is in the plugin's directory for the job, by the
// <jobid>/.<jobid> in its path. The pin of the namespace (<basepath>/<jobid>/.ns) is not one.
func slurmMounts(job []mount, jobID uint32) []PrivateMount {
	id := strconv.FormatUint(uint64(jobID), 10)

	var mounts []PrivateMount
	for _, m := range job {
		if !ofJob(m.Root, id) {
			continue
		}
		mounts = append(mounts, PrivateMount{Mountpoint: m.Mountpoint, FSType: m.FSType, Root: m.Root})
	}
	return mounts
}

// ofJob tells a path with <id>/.<id> in it, followed by something
func ofJob(path, id string) bool {
	components := strings.Split(path, "/")
	for i := 0; i+2 < len(components); i++ {
		if components[i] == id && components[i+1] == "."+id && components[i+2] != "" {
			return true
		}
	}
	return false
}

func readMountinfo(path string) ([]mount, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	return parseMountinfo(file)
}

// parseMountinfo returns the mounts in the order listed, skipping any line it can't make sense of
func parseMountinfo(mountinfo io.Reader) ([]mount, error) {
	var mounts []mount

	scanner := bufio.NewScanner(mountinfo)
	for scanner.Scan() {
		// 412 98 0:4 pid:[4026532715] /var/spool/slurmd/1234/.ns/pid rw,nosuid shared:1 - nsfs nsfs rw
		// (0) (1) (2) (3: root)       (4: mountpoint)                ...optional...  (-) (fstype)
		fields := strings.Fields(scanner.Text())
		if len(fields) < 7 {
			continue
		}
		sep := -1
		for i := 6; i < len(fields); i++ {
			if fields[i] == "-" {
				sep = i
				break
			}
		}
		if sep < 0 || sep+1 >= len(fields) {
			continue
		}
		id, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		mounts = append(mounts, mount{
			ID:         id,
			Device:     fields[2],
			Root:       unescapeMountinfo(fields[3]),
			Mountpoint: unescapeMountinfo(fields[4]),
			FSType:     fields[sep+1],
		})
	}

	return mounts, scanner.Err()
}

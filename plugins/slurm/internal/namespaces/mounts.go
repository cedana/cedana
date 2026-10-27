package namespaces

// Recognizes the directories SLURM's namespace plugin gives the job.
//
// job_container/tmpfs (namespace/tmpfs from 26.05) runs the job in a mount
// namespace of its own with a directory of the job's for each entry of Dirs
// (/tmp,/dev/shm by default): a bind mount from <basepath>/<jobid>/.<jobid>/
// for /tmp, a new tmpfs for /dev/shm. Both go away with the job, and a dump
// from inside the namespace records their files by path only, so what is in
// them goes into the dump (CED-2532).

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/rs/zerolog/log"
)

// The node's mounts. Readable by any user, unlike init's namespace links.
const initMountinfo = "/proc/1/mountinfo"

// The one kind of filesystem that goes away with its mount namespace
const disposableFSType = "tmpfs"

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
	// Root of the mount in its filesystem: the plugin's directory for a bind mount, / for a tmpfs
	Root string
	// major:minor of the mount's filesystem
	Device string
}

// SlurmMounts returns the mounts of pid that SLURM's namespace plugin made for the job,
// in the order they are mounted.
func SlurmMounts(pid uint32, jobID uint32) ([]PrivateMount, error) {
	jobPath := fmt.Sprintf("/proc/%d/mountinfo", pid)
	job, err := readMountinfo(jobPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", jobPath, err)
	}
	node, err := readMountinfo(initMountinfo)
	if err != nil {
		// e.g. hidepid on /proc: the path rule alone, which misses the tmpfs at /dev/shm
		log.Warn().Err(err).Msgf("failed to read %s, recognizing the job's private mounts by their path alone", initMountinfo)
		node = nil
	}
	return slurmMounts(job, node, jobID), nil
}

// slurmMounts picks the mounts the job has to itself: those rooted in the plugin's directory
// for the job (<jobid>/.<jobid> in the path), and a tmpfs the node has no mount of. A new mount
// namespace copies its parent's mounts with their device and root, so a tmpfs that is nowhere
// on the node was made for the job and goes away with it. Only a tmpfs: a filesystem that
// outlives the job, such as an NFS share mounted for it, must not be overwritten by the restore.
// Without the node's mounts, the path alone decides. See CED-2532.
func slurmMounts(job, node []mount, jobID uint32) []PrivateMount {
	id := strconv.FormatUint(uint64(jobID), 10)

	var mounts []PrivateMount
	for _, m := range job {
		var how string
		switch {
		case ofJob(m.Root, id):
			how = "the root of the mount is in the plugin's directory for the job"
		case node != nil && m.FSType == disposableFSType && !onNode(m, node):
			how = "a tmpfs the node has no mount of"
		default:
			continue
		}
		log.Debug().Str("mountpoint", m.Mountpoint).Str("device", m.Device).Str("root", m.Root).Msgf("private mount of the job: %s", how)
		mounts = append(mounts, PrivateMount{Mountpoint: m.Mountpoint, FSType: m.FSType, Root: m.Root, Device: m.Device})
	}
	return mounts
}

// onNode tells whether the node has a mount of m's filesystem that holds m's root
func onNode(m mount, node []mount) bool {
	for _, n := range node {
		if n.Device != m.Device {
			continue
		}
		if n.Root == m.Root || n.Root == "/" || strings.HasPrefix(m.Root, n.Root+"/") {
			return true
		}
	}
	return false
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

// ownMountNamespace tells whether pid is in a mount namespace other than init's, from the
// mounts alone, which any user can read: a new namespace copies every mount under new mount
// IDs, so the ID of pid's root mount is one init's mounts lack.
func ownMountNamespace(pid uint32) (bool, error) {
	jobPath := fmt.Sprintf("/proc/%d/mountinfo", pid)
	job, err := readMountinfo(jobPath)
	if err != nil {
		return false, fmt.Errorf("failed to read %s: %w", jobPath, err)
	}
	node, err := readMountinfo(initMountinfo)
	if err != nil {
		return false, fmt.Errorf("failed to read %s: %w", initMountinfo, err)
	}
	return ownMountNamespaceOf(job, node)
}

func ownMountNamespaceOf(job, node []mount) (bool, error) {
	rootID := -1
	for _, m := range job {
		if m.Mountpoint == "/" {
			rootID = m.ID
			break
		}
	}
	if rootID < 0 {
		return false, errors.New("no mount at / in the mounts of the process")
	}
	for _, m := range node {
		if m.ID == rootID {
			return false, nil
		}
	}
	return true, nil
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

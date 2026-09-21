package namespaces

// Recognizes the mounts that exist only in the job's mount namespace.
//
// Running CRIU inside an external mount namespace takes care of the mount tree,
// but not of what is in it: a tmpfs mounted by the launcher (e.g. a PAM module giving
// each session its own /var/tmp) lives and dies with the namespace.

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

// PrivateMount is a mount of the job that the host does not have
type PrivateMount struct {
	Mountpoint string
	FSType     string
}

// RecognizePrivateMounts returns the mounts of pid that differ from what the host has
// mounted at the same place. Returns nothing if pid is in the host's mount namespace.
//
// NOTE: Mount IDs can't be used for this. A new mount namespace is a copy of the
// parent's where every mount is given a new ID, so by ID all of them look private.
func RecognizePrivateMounts(pid uint32) ([]PrivateMount, error) {
	jobPath := fmt.Sprintf("/proc/%d/mountinfo", pid)
	job, err := readMountinfo(jobPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", jobPath, err)
	}

	host, err := readMountinfo("/proc/1/mountinfo")
	if err != nil {
		host, err = readMountinfo("/proc/self/mountinfo")
		if err != nil {
			return nil, fmt.Errorf("failed to read host mountinfo: %w", err)
		}
	}

	return privateMounts(job, host), nil
}

// privateMounts compares what is visible at each mountpoint, i.e. the topmost mount.
// It's the same mount if it's the same part (root) of the same filesystem instance (device).
func privateMounts(job, host []mount) []PrivateMount {
	hostTop := map[string]mount{}
	for _, m := range host {
		hostTop[m.Mountpoint] = m
	}

	jobTop := map[string]mount{}
	var order []string
	for _, m := range job {
		if _, ok := jobTop[m.Mountpoint]; !ok {
			order = append(order, m.Mountpoint)
		}
		jobTop[m.Mountpoint] = m
	}

	var private []PrivateMount
	for _, mountpoint := range order {
		m := jobTop[mountpoint]
		if h, ok := hostTop[mountpoint]; ok && h.Device == m.Device && h.Root == m.Root {
			continue
		}
		private = append(private, PrivateMount{Mountpoint: m.Mountpoint, FSType: m.FSType})
	}

	return private
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

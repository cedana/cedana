package namespaces

// Recognizes which namespaces of a slurm job are external, i.e. created and
// held open by slurm rather than by the job itself.
//
// Slurm's namespace/linux plugin pins each namespace it creates by bind-mounting
// /proc/<pid>/ns/<type> onto <basepath>/<jobid>/.ns/<type>. Such a pin shows up
// in mountinfo as an nsfs mount whose root is '<type>:[<inode>]'.
// https://github.com/SchedMD/slurm/blob/035cb8f0b5d1fb6a375b27f2ecde106b84473ed5/src/plugins/namespace/linux/namespace_linux.c#L788-L805

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/opencontainers/runc/libcontainer/configs"
	"github.com/rs/zerolog/log"
	"golang.org/x/sys/unix"
)

const (
	nsfsType = "nsfs"

	// How far up the process tree to look for a mount namespace that can see the pins
	maxMountinfoAncestors = 8
)

var nsfsRootPattern = regexp.MustCompile(`^(\w+):\[(\d+)\]$`)

// RecognizedNamespace is a namespace the job lives in that is held open by
// something outside the job (a bind-mounted nsfs file).
type RecognizedNamespace struct {
	Type  configs.NamespaceType
	Inode uint64
	Path  string // nsfs pin, e.g. <basepath>/<jobid>/.ns/pid
}

// RecognizeExternalNamespaces returns the namespaces of pid that differ from
// the host's and are pinned by an nsfs mount. Returns nothing if the process
// is simply running in the host's namespaces.
func RecognizeExternalNamespaces(pid uint32) ([]RecognizedNamespace, error) {
	if pid == 0 {
		return nil, fmt.Errorf("invalid pid %d", pid)
	}

	jobInodes := map[configs.NamespaceType]uint64{}
	hostInodes := map[configs.NamespaceType]uint64{}

	for _, t := range configs.NamespaceTypes() {
		nsPath := nsPathOf(t, pid)
		if nsPath == "" {
			continue
		}
		ino, err := nsInode(nsPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue // namespace type not supported by this kernel
			}
			return nil, fmt.Errorf("failed to stat %s: %w", nsPath, err)
		}
		jobInodes[t] = ino

		hostIno, err := hostNsInode(t)
		if err != nil {
			return nil, err
		}
		hostInodes[t] = hostIno
	}

	pins := pinnedNamespaces(pid)

	return classifyNamespaces(jobInodes, hostInodes, pins), nil
}

// classifyNamespaces picks out the external namespaces, in the stable order of configs.NamespaceTypes().
//
//	same inode as host   -> host namespace, ignored
//	differs, pinned      -> external
//	differs, not pinned  -> private to the job, left for CRIU to dump as usual
func classifyNamespaces(jobInodes, hostInodes map[configs.NamespaceType]uint64, pins map[uint64]string) []RecognizedNamespace {
	var recognized []RecognizedNamespace

	for _, t := range configs.NamespaceTypes() {
		ino, ok := jobInodes[t]
		if !ok {
			continue
		}
		if hostIno, ok := hostInodes[t]; ok && hostIno == ino {
			continue
		}
		path, ok := pins[ino]
		if !ok {
			log.Warn().
				Str("type", configs.NsName(t)).
				Uint64("inode", ino).
				Msg("namespace differs from host but no nsfs pin is visible, treating it as private to the job")
			continue
		}
		recognized = append(recognized, RecognizedNamespace{Type: t, Inode: ino, Path: path})
	}

	return recognized
}

// pinnedNamespaces returns inode -> mountpoint for all visible nsfs pins.
//
// Slurm makes <basepath>/<jobid> a private mount, so the pins are only visible
// in the mount namespace where slurmstepd created them. We check ours, init's, and
// then walk up from the job. The job's own mount namespace is of no use, as the
// pins are created in its parent.
func pinnedNamespaces(pid uint32) map[uint64]string {
	pins := map[uint64]string{}
	seen := map[uint64]bool{} // mount namespaces already read

	read := func(proc string) {
		if ino, err := nsInode(proc + "/ns/mnt"); err == nil {
			if seen[ino] {
				return
			}
			seen[ino] = true
		}
		file, err := os.Open(proc + "/mountinfo")
		if err != nil {
			log.Trace().Err(err).Msgf("failed to open %s/mountinfo", proc)
			return
		}
		defer file.Close()
		if err := pinnedNamespacesFromReader(file, pins); err != nil {
			log.Trace().Err(err).Msgf("failed to parse %s/mountinfo", proc)
		}
	}

	read("/proc/self")
	read("/proc/1")

	for i, p := 0, parentOf(pid); i < maxMountinfoAncestors && p > 1; i, p = i+1, parentOf(p) {
		read(fmt.Sprintf("/proc/%d", p))
	}

	return pins
}

// pinnedNamespacesFromReader adds the nsfs pins found in mountinfo to pins. First pin for an inode wins.
func pinnedNamespacesFromReader(mountinfo io.Reader, pins map[uint64]string) error {
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
		if sep < 0 || sep+1 >= len(fields) || fields[sep+1] != nsfsType {
			continue
		}
		_, ino, ok := parseNsfsRoot(fields[3])
		if !ok {
			continue
		}
		if _, ok := pins[ino]; !ok {
			pins[ino] = unescapeMountinfo(fields[4])
		}
	}
	return scanner.Err()
}

// parseNsfsRoot parses the root of an nsfs mount, e.g. 'pid:[4026532715]'
func parseNsfsRoot(root string) (t configs.NamespaceType, inode uint64, ok bool) {
	match := nsfsRootPattern.FindStringSubmatch(root)
	if match == nil {
		return "", 0, false
	}
	t, ok = nsTypeFromName(match[1])
	if !ok {
		return "", 0, false
	}
	inode, err := strconv.ParseUint(match[2], 10, 64)
	if err != nil {
		return "", 0, false
	}
	return t, inode, true
}

// nsTypeFromName is the inverse of configs.NsName
func nsTypeFromName(name string) (configs.NamespaceType, bool) {
	for _, t := range configs.NamespaceTypes() {
		if configs.NsName(t) == name {
			return t, true
		}
	}
	return "", false
}

func nsInode(path string) (uint64, error) {
	var st unix.Stat_t
	if err := unix.Stat(path, &st); err != nil {
		return 0, err
	}
	return uint64(st.Ino), nil
}

// hostNsInode prefers init's namespace, falling back to ours if we're not allowed to look
func hostNsInode(t configs.NamespaceType) (uint64, error) {
	ino, err := nsInode(nsPathOf(t, 1))
	if err == nil {
		return ino, nil
	}
	self := nsPathOf(t, uint32(os.Getpid()))
	ino, err = nsInode(self)
	if err != nil {
		return 0, fmt.Errorf("failed to stat %s: %w", self, err)
	}
	return ino, nil
}

func parentOf(pid uint32) uint32 {
	status, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(status), "\n") {
		if rest, ok := strings.CutPrefix(line, "PPid:"); ok {
			ppid, err := strconv.ParseUint(strings.TrimSpace(rest), 10, 32)
			if err != nil {
				return 0
			}
			return uint32(ppid)
		}
	}
	return 0
}

// unescapeMountinfo decodes the octal escapes (\040 etc.) the kernel uses in mountinfo paths
func unescapeMountinfo(path string) string {
	if !strings.Contains(path, `\`) {
		return path
	}
	var b strings.Builder
	for i := 0; i < len(path); i++ {
		if path[i] == '\\' && i+3 < len(path) {
			if v, err := strconv.ParseUint(path[i+1:i+4], 8, 8); err == nil {
				b.WriteByte(byte(v))
				i += 3
				continue
			}
		}
		b.WriteByte(path[i])
	}
	return b.String()
}

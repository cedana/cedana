package namespaces

import (
	"fmt"
	"strings"

	"github.com/opencontainers/runc/libcontainer/configs"
)

func CriuNsToKey(t configs.NamespaceType) string {
	return "extRoot" + strings.ToTitle(
		configs.NsName(t),
	) + "NS"
}

func nsPathOf(t configs.NamespaceType, pid uint32) string {
	// for each namespace type, return the path to the namespace file in /proc/pid/ns
	switch t {
	case configs.NEWNS:
		return fmt.Sprintf("/proc/%d/ns/mnt", pid)
	case configs.NEWUTS:
		return fmt.Sprintf("/proc/%d/ns/uts", pid)
	case configs.NEWIPC:
		return fmt.Sprintf("/proc/%d/ns/ipc", pid)
	case configs.NEWUSER:
		return fmt.Sprintf("/proc/%d/ns/user", pid)
	case configs.NEWNET:
		return fmt.Sprintf("/proc/%d/ns/net", pid)
	case configs.NEWPID:
		return fmt.Sprintf("/proc/%d/ns/pid", pid)
	default:
		return ""
	}
}

package namespaces

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/opencontainers/runc/libcontainer/configs"
	"github.com/spf13/afero"
)

// Records the external namespaces added to a dump, so that restore inherits exactly those
const EXTERNAL_NAMESPACES_FILE = "external_namespaces.json"

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

// criuSupportsExternal checks if CRIU can handle the namespace type as an external namespace.
// NOTE: CRIU only supports this for net and pid. '--external mnt[]' exists, but
// means an external bind mount, not an external mount namespace.
func criuSupportsExternal(t configs.NamespaceType, version int) (ok bool, reason string) {
	var minVersion int
	switch t {
	case configs.NEWNET:
		minVersion = 31100
	case configs.NEWPID:
		minVersion = 31500
	default:
		return false, fmt.Sprintf("CRIU does not support external %s namespaces", configs.NsName(t))
	}
	if version < minVersion {
		return false, fmt.Sprintf("CRIU version is less than %d", minVersion)
	}
	return true, ""
}

// CRIU expects the information about an external namespace
// like this: --external <TYPE>[<inode>]:<key>
// This <key> is always 'extRoot<TYPE>NS'.
func addExternalNamespace(req *daemon.DumpReq, t configs.NamespaceType, inode uint64) {
	external := fmt.Sprintf("%s[%d]:%s", configs.NsName(t), inode, CriuNsToKey(t))

	if req.Criu == nil {
		req.Criu = &criu_proto.CriuOpts{}
	}

	for _, existing := range req.Criu.External {
		if existing == external {
			return
		}
	}

	req.Criu.External = append(req.Criu.External, external)
}

func saveExternalNamespaces(fs afero.Fs, nsTypes []configs.NamespaceType) error {
	names := make([]string, 0, len(nsTypes))
	for _, t := range nsTypes {
		names = append(names, configs.NsName(t))
	}

	file, err := fs.Create(EXTERNAL_NAMESPACES_FILE)
	if err != nil {
		return err
	}
	defer file.Close()

	return json.NewEncoder(file).Encode(names)
}

// loadExternalNamespaces returns nothing if the dump has no external namespaces recorded
func loadExternalNamespaces(fs afero.Fs) ([]configs.NamespaceType, error) {
	file, err := fs.Open(EXTERNAL_NAMESPACES_FILE)
	if err != nil {
		if exists, _ := afero.Exists(fs, EXTERNAL_NAMESPACES_FILE); !exists {
			return nil, nil
		}
		return nil, err
	}
	defer file.Close()

	contents, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}

	var names []string
	if err := json.Unmarshal(contents, &names); err != nil {
		return nil, err
	}

	nsTypes := make([]configs.NamespaceType, 0, len(names))
	for _, name := range names {
		t, ok := nsTypeFromName(name)
		if !ok {
			return nil, fmt.Errorf("unknown namespace type %q", name)
		}
		nsTypes = append(nsTypes, t)
	}

	return nsTypes, nil
}

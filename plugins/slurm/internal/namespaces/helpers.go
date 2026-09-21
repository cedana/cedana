package namespaces

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/opencontainers/runc/libcontainer/configs"
	"github.com/spf13/afero"
	"golang.org/x/sys/unix"
)

// Records how the external namespaces were handled on dump, so that restore mirrors it exactly
const EXTERNAL_NAMESPACES_FILE = "external_namespaces.json"

type Handling string

const (
	// Namespace is left out of the dump using --external, and inherited on restore using --inherit-fd
	HandlingExternal Handling = "external"
	// CRIU runs inside the namespace, for both dump and restore, so it's unaware of it
	HandlingEnter Handling = "enter"
)

type ExternalNamespace struct {
	Type     configs.NamespaceType
	Handling Handling
	Holder   HolderKind
}

type externalNamespaceJSON struct {
	Type     string     `json:"type"`
	Handling Handling   `json:"handling"`
	Holder   HolderKind `json:"holder,omitempty"`
}

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

// handlingFor decides what can be done about an external namespace of this type, if anything
func handlingFor(t configs.NamespaceType, version int) (handling Handling, reason string) {
	if t == configs.NEWNS {
		return HandlingEnter, ""
	}
	if ok, reason := criuSupportsExternal(t, version); !ok {
		return "", reason
	}
	return HandlingExternal, ""
}

// inHostNamespace is conservative, any failure to tell means no
func inHostNamespace(t configs.NamespaceType, pid uint32) bool {
	ino, err := nsInode(nsPathOf(t, pid))
	if err != nil {
		return false
	}
	hostIno, err := hostNsInode(t)
	if err != nil {
		return false
	}
	return ino == hostIno
}

// visibleInNamespace checks that path is the very same file for us as for pid in its mount namespace
func visibleInNamespace(pid uint32, path string) (bool, error) {
	var ours, theirs unix.Stat_t
	if err := unix.Stat(path, &ours); err != nil {
		return false, fmt.Errorf("failed to stat %s: %w", path, err)
	}
	if err := unix.Stat(fmt.Sprintf("/proc/%d/root%s", pid, path), &theirs); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("failed to stat %s as seen by %d: %w", path, pid, err)
	}
	return ours.Dev == theirs.Dev && ours.Ino == theirs.Ino, nil
}

func saveExternalNamespaces(fs afero.Fs, namespaces []ExternalNamespace) error {
	entries := make([]externalNamespaceJSON, 0, len(namespaces))
	for _, ns := range namespaces {
		entries = append(entries, externalNamespaceJSON{
			Type:     configs.NsName(ns.Type),
			Handling: ns.Handling,
			Holder:   ns.Holder,
		})
	}

	file, err := fs.Create(EXTERNAL_NAMESPACES_FILE)
	if err != nil {
		return err
	}
	defer file.Close()

	return json.NewEncoder(file).Encode(entries)
}

// loadExternalNamespaces returns nothing if the dump has no external namespaces recorded
func loadExternalNamespaces(fs afero.Fs) ([]ExternalNamespace, error) {
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

	var raw []json.RawMessage
	if err := json.Unmarshal(contents, &raw); err != nil {
		return nil, err
	}

	namespaces := make([]ExternalNamespace, 0, len(raw))
	for _, r := range raw {
		var entry externalNamespaceJSON

		// Used to be a plain list of names, from when --external was the only handling
		if err := json.Unmarshal(r, &entry.Type); err == nil {
			entry.Handling = HandlingExternal
		} else if err := json.Unmarshal(r, &entry); err != nil {
			return nil, err
		}

		t, ok := nsTypeFromName(entry.Type)
		if !ok {
			return nil, fmt.Errorf("unknown namespace type %q", entry.Type)
		}
		switch entry.Handling {
		case HandlingExternal, HandlingEnter:
		default:
			return nil, fmt.Errorf("unknown handling %q for %s namespace", entry.Handling, entry.Type)
		}

		namespaces = append(namespaces, ExternalNamespace{Type: t, Handling: entry.Handling, Holder: entry.Holder})
	}

	return namespaces, nil
}

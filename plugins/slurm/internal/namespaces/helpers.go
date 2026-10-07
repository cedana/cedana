package namespaces

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/opencontainers/runc/libcontainer/configs"
	"github.com/spf13/afero"
)

// Records how the external namespaces were handled on dump, so that restore mirrors it exactly
const EXTERNAL_NAMESPACES_FILE = "external_namespaces.json"

type Handling string

const (
	// Namespace is left out of the dump using --external, and inherited on restore using --inherit-fd
	HandlingExternal Handling = "external"
	// The job has the namespace to itself, and was dumped from inside it, so the dump has no
	// trace of it. The restore has to run from inside the new job's as well, or CRIU would
	// restore into ours. Only for mnt: CRIU has no notion of an external mount namespace.
	HandlingInside Handling = "inside"
	// Recorded by earlier dumps for a mount namespace CRIU was put inside of. The same thing
	// to a restore as HandlingInside. Still read, for those dumps.
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

// handlingFor decides what can be done about an external namespace of this type, if anything.
// A mount namespace can't be told to CRIU, which has no notion of an external one, nor entered
// (that takes CAP_SYS_ADMIN, which the job's user doesn't have): a job in one of its own is
// dumped from inside it, see AddRecognizedExternalNamespacesForDump.
func handlingFor(t configs.NamespaceType, version int) (handling Handling, reason string) {
	if t == configs.NEWNS {
		return HandlingInside, ""
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

// inNamespaceOf tells whether we are in the namespace of this type that pid is in
func inNamespaceOf(t configs.NamespaceType, pid uint32) (bool, error) {
	ours, err := nsInode(nsPathOf(t, uint32(os.Getpid())))
	if err != nil {
		return false, err
	}
	theirs, err := nsInode(nsPathOf(t, pid))
	if err != nil {
		return false, err
	}
	return ours == theirs, nil
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
		// Anything else (e.g. a failing streamer) is not the same as there being none
		if errors.Is(err, os.ErrNotExist) {
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
		case HandlingExternal, HandlingInside, HandlingEnter:
		default:
			return nil, fmt.Errorf("unknown handling %q for %s namespace", entry.Handling, entry.Type)
		}

		namespaces = append(namespaces, ExternalNamespace{Type: t, Handling: entry.Handling, Holder: entry.Holder})
	}

	return namespaces, nil
}

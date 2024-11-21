package plugins

// Defines the plugin manager interface

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const featureVersion Feature[string] = "Version"

type (
	Type   int
	Status int
)

const (
	Unimplemented Type = iota // Go plugin that is not yet implemented
	Deprecated                // Go plugin that is no longer maintained
	Experimental              // Go plugin that is not yet stable
	External                  // Not a Go plugin
	Supported                 // Go plugin that is supported by Cedana
)

const (
	Unknown Status = iota
	Available
	Installed
)

// Represents plugin information
type Plugin struct {
	Name          string
	Type          Type
	Status        Status
	Version       string
	LatestVersion string
	Libraries     []string
	Binaries      []string
	Size          int64 // in bytes
	Dependencies  []string
}

type Manager interface {
	List(...Status) ([]Plugin, error)
	Install(names []string) (chan int, chan string, chan error)
	Remove(names []string) (chan int, chan string, chan error)
}

/////////////////
//// Methods ////
/////////////////

func (t Type) String() string {
	switch t {
	case Supported:
		return "supported"
	case Experimental:
		return "experimental"
	case Deprecated:
		return "deprecated"
	case Unimplemented:
		return "unimplemented"
	default:
		return "-"
	}
}

func (s Status) String() string {
	switch s {
	case Available:
		return "available"
	case Installed:
		return "installed"
	default:
		return "-"
	}
}

// SyncVersion fetches the version of the locally installed plugin
func (p *Plugin) SyncVersion() {
	version := "unknown"
	switch p.Type {
	case Supported: // can fetch from symbol
		featureVersion.IfAvailable(func(name string, versionSym string) error {
			version = strings.TrimSpace(versionSym)
			return nil
		}, p.Name)
	case External: // can fetch by executing first binary with flag
		if len(p.Binaries) < 1 {
			break
		}
		binary := p.Binaries[0]
		cmd := exec.Command(binary, "--version")
		if out, err := cmd.Output(); err == nil {
			version = strings.TrimSpace(string(out))
		} else {
			version = "error"
		}
	}
	p.Version = version
}

// Syncs plugin information with local info, whether it is installed or not.
// Also fetches the local installed version.
func (p *Plugin) SyncInstalled() {
	// Check if all plugin files are installed
	found := 0
	for _, file := range p.Libraries {
		if _, err := os.Stat(filepath.Join(LibDir, file)); err != nil {
			continue
		}
		found += 1
	}
	if found < len(p.Libraries) {
		return
	}

	found = 0
	for _, file := range p.Binaries {
		if _, err := os.Stat(filepath.Join(BinDir, file)); err != nil {
			continue
		}
		found += 1
	}
	if found < len(p.Binaries) {
		return
	}
	p.Status = Installed
	p.SyncVersion()
}

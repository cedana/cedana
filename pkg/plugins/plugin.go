package plugins

// Defines the plugin type

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/cedana/cedana/pkg/utils"
)

var featureVersion = Feature[string]{"Version", "version"}

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
	Outdated
)

// Represents plugin information
type Plugin struct {
	Name          string    `json:"name"`
	Type          Type      `json:"type"`
	Status        Status    `json:"status"`
	Version       string    `json:"version"`
	LatestVersion string    `json:"latest_version"`
	Libraries     []Binary  `json:"libraries"`
	Binaries      []Binary  `json:"binaries"`
	Size          int64     `json:"size"` // in bytes
	PublishedAt   time.Time `json:"published_at"`
}

type Binary struct {
	Name     string `json:"name"`
	Checksum string `json:"checksum"` // MD5
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
	case Outdated:
		return "outdated"
	default:
		return "unknown"
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
		cmd := exec.Command(binary.Name, "--version")
		cmd.Env = append(os.Environ(), "PATH="+BinDir+":"+os.Getenv("PATH"))
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
	var err error
	var s os.FileInfo
	found := 0
	size := int64(0)

	for i, file := range p.Libraries {
		path := filepath.Join(LibDir, file.Name)
		if s, err = os.Stat(path); err != nil {
			continue
		}
		found += 1
		size += s.Size()
		sum, _ := utils.FileMD5Sum(path)
		p.Libraries[i].Checksum = string(sum)
	}
	if found < len(p.Libraries) {
		return
	}

	found = 0
	for i, file := range p.Binaries {
		path := filepath.Join(BinDir, file.Name)
		if s, err = os.Stat(path); err != nil {
			continue
		}
		found += 1
		size += s.Size()
		sum, _ := utils.FileMD5Sum(path)
		p.Binaries[i].Checksum = string(sum)
	}
	if found < len(p.Binaries) {
		return
	}

	p.Status = Installed
	p.Size = size
	p.SyncVersion()
}

// BinaryPaths returns the full paths of the plugin binaries
func (p *Plugin) BinaryPaths() []string {
	paths := make([]string, len(p.Binaries))
	for i, bin := range p.Binaries {
		paths[i] = filepath.Join(BinDir, bin.Name)
	}
	return paths
}

// LibraryPaths returns the full paths of the plugin libraries
func (p *Plugin) LibraryPaths() []string {
	paths := make([]string, len(p.Libraries))
	for i, lib := range p.Libraries {
		paths[i] = filepath.Join(LibDir, lib.Name)
	}
	return paths
}

// Checksum returns the concatenated checksum of all libraries and binaries
func (p *Plugin) Checksum() string {
	total := ""
	for _, lib := range p.Libraries {
		total += lib.Checksum
	}
	for _, bin := range p.Binaries {
		total += bin.Checksum
	}
	return total
}

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
	UNIMPLEMENTED Type = iota // Go plugin that is not yet implemented
	DEPRECATED                // Go plugin that is no longer maintained
	EXPERIMENTAL              // Go plugin that is not yet stable
	EXTERNAL                  // Not a Go plugin
	SUPPORTED                 // Go plugin that is supported by Cedana
)

const (
	UNKNOWN Status = iota
	AVAILABLE
	INSTALLED
	OUTDATED
)

// Represents plugin information
type Plugin struct {
	Name             string    `json:"name"`
	Type             Type      `json:"type"`
	Status           Status    `json:"status"`
	Version          string    `json:"version"`
	AvailableVersion string    `json:"latest_version"`
	Libraries        []Binary  `json:"libraries"`
	Binaries         []Binary  `json:"binaries"`
	Size             int64     `json:"size"` // in bytes
	PublishedAt      time.Time `json:"published_at"`
}

type Binary struct {
	Name       string `json:"name"`
	Checksum   string `json:"checksum"`     // MD5
	InstallDir string `json:"install_path"` // Fixed path where the binary must be installed
}

/////////////////
//// Methods ////
/////////////////

func (t Type) String() string {
	switch t {
	case SUPPORTED:
		return "supported"
	case EXPERIMENTAL:
		return "experimental"
	case DEPRECATED:
		return "deprecated"
	case UNIMPLEMENTED:
		return "unimplemented"
	default:
		return "-"
	}
}

func (s Status) String() string {
	switch s {
	case AVAILABLE:
		return "available"
	case INSTALLED:
		return "installed"
	case OUTDATED:
		return "outdated"
	default:
		return "unknown"
	}
}

// SyncVersion fetches the version of the locally installed plugin
func (p *Plugin) SyncVersion() {
	version := "unknown"
	switch p.Type {
	case SUPPORTED: // can fetch from symbol
		featureVersion.IfAvailable(func(name string, versionSym string) error {
			version = strings.TrimSpace(versionSym)
			return nil
		}, p.Name)
	case EXTERNAL: // can fetch by executing first binary with flag
		if len(p.Binaries) < 1 {
			break
		}
		binary := p.Binaries[0]
		cmd := exec.Command(binary.Name, "--version")
		cmd.Env = append(os.Environ(), "PATH="+BinDir+":"+os.Getenv("PATH"))
		if out, err := cmd.Output(); err == nil {
			if v := strings.TrimSpace(string(out)); v != "" {
				version = v
			}
		}
	}

	if version != "unknown" {
		// Get first line
		if i := strings.Index(version, "\n"); i > 0 {
			version = version[:i]
		}

		// Get last word
		if i := strings.LastIndex(version, " "); i > 0 {
			version = version[i+1:]
		}

		// Add 'v' prefix if missing, and version starts with a number
		if version != "" && version[0] >= '0' && version[0] <= '9' {
			version = "v" + version
		}
	}

	// If still unknown, get version from parent plugin
	if version == "unknown" && strings.Contains(p.Name, "/") {
		parentName := strings.Split(p.Name, "/")[0]
		var parent *Plugin
		for _, plugin := range Registry {
			if plugin.Name == parentName {
				parent = &plugin
				break
			}
		}
		if parent != nil {
			parent.SyncVersion()
			version = parent.Version
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
		var path string
		if file.InstallDir == "" {
			path = filepath.Join(LibDir, file.Name)
		} else {
			path = filepath.Join(file.InstallDir, file.Name)
		}
		if s, err = os.Stat(path); err != nil || s.IsDir() {
			continue
		}
		found += 1
		size += s.Size()
		p.Libraries[i].Checksum, _ = utils.FileMD5Sum(path)
	}
	if found < len(p.Libraries) {
		return
	}

	found = 0
	for i, file := range p.Binaries {
		var path string
		if file.InstallDir == "" {
			path = filepath.Join(BinDir, file.Name)
		} else {
			path = filepath.Join(file.InstallDir, file.Name)
		}
		if s, err = os.Stat(path); err != nil || s.IsDir() {
			continue
		}
		found += 1
		size += s.Size()
		p.Binaries[i].Checksum, _ = utils.FileMD5Sum(path)
	}
	if found < len(p.Binaries) {
		return
	}
	p.Status = INSTALLED
	p.Size = size
	p.SyncVersion()
}

// BinaryPaths returns the full paths of the plugin binaries
func (p *Plugin) BinaryPaths() []string {
	paths := make([]string, len(p.Binaries))
	for i, bin := range p.Binaries {
		if bin.InstallDir != "" {
			paths[i] = filepath.Join(bin.InstallDir, bin.Name)
		} else {
			paths[i] = filepath.Join(BinDir, bin.Name)
		}
	}
	return paths
}

// LibraryPaths returns the full paths of the plugin libraries
func (p *Plugin) LibraryPaths() []string {
	paths := make([]string, len(p.Libraries))
	for i, lib := range p.Libraries {
		if lib.InstallDir != "" {
			paths[i] = filepath.Join(lib.InstallDir, lib.Name)
		} else {
			paths[i] = filepath.Join(LibDir, lib.Name)
		}
	}
	return paths
}

// Checksum returns the concatenated checksum of all libraries and binaries
func (p *Plugin) Checksum() string {
	var total strings.Builder
	for _, lib := range p.Libraries {
		total.WriteString(lib.Checksum)
	}
	for _, bin := range p.Binaries {
		total.WriteString(bin.Checksum)
	}
	return total.String()
}

func (p *Plugin) IsInstalled() bool {
	if p == nil {
		return false
	}
	return p.Status == INSTALLED || p.Status == OUTDATED
}

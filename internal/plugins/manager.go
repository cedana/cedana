package plugins

import (
	"os"
	"path/filepath"

	"github.com/cedana/cedana/internal/config"
)

// Defines the plugin manager interface

// Plugins that are supported by Cedana
var Plugins = []Plugin{
	// Container runtimes
	{
		Name:      "runc",
		Type:      Supported,
		Status:    Unknown,
		Libraries: []string{"libcedana-runc.so"},
	},
	{
		Name:         "containerd",
		Type:         Unimplemented,
		Status:       Unknown,
		Libraries:    []string{"libcedana-containerd.so"},
		Dependencies: []string{"runc"},
	},
	{
		Name:      "crio",
		Type:      Unimplemented,
		Status:    Unknown,
		Libraries: []string{"libcedana-crio.so"},
	},
	{
		Name:      "kata",
		Type:      Unimplemented,
		Status:    Unknown,
		Libraries: []string{"libcedana-kata.so"},
	},
	{
		Name:      "docker",
		Type:      Unimplemented,
		Status:    Unknown,
		Libraries: []string{"libcedana-docker.so"},
	},

	// Checkpoint/Restore
	{
		Name:      "gpu",
		Type:      External,
		Status:    Unknown,
		Libraries: []string{"libcedana-gpu.so"},
		Binaries:  []string{"cedana-gpu-controller"},
	},
	{
		Name:      "streamer",
		Type:      Experimental,
		Status:    Unknown,
		Libraries: []string{"libcedana-streamer.so"},
		Binaries:  []string{"cedana-image-streamer"},
	},
}

type (
	Type   int
	Status int
)

const (
	Supported     Type = iota // Go plugin that is supported by Cedana
	Experimental              // Go plugin that is not yet stable
	Deprecated                // Go plugin that is no longer maintained
	External                  // Not a Go plugin
	Unimplemented             // Go plugin that is not yet implemented
)

const (
	Installed Status = iota
	Available
	Unknown
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

func (t Type) String() string {
	switch t {
	case Supported:
		return "Supported"
	case Experimental:
		return "Experimental"
	case Deprecated:
		return "Deprecated"
	case Unimplemented:
		return "Unimplemented"
	default:
		return "-"
	}
}

func (s Status) String() string {
	switch s {
	case Available:
		return "Available"
	case Installed:
		return "Installed"
	default:
		return "-"
	}
}

// Syncs plugin information with local info
func SyncInstalled(p *Plugin) {
	// Check if all plugin files are installed
	found := 0
	for _, file := range p.Libraries {
		if _, err := os.Stat(filepath.Join(config.Get(config.PLUGINS_LIB_DIR), file)); err != nil {
			continue
		}
		found += 1
	}
	if found < len(p.Libraries) {
		return
	}

	found = 0
	for _, file := range p.Binaries {
		if _, err := os.Stat(filepath.Join(config.Get(config.PLUGINS_BIN_DIR), file)); err != nil {
			continue
		}
		found += 1
	}
	if found < len(p.Binaries) {
		return
	}
	p.Status = Installed

	// TODO: Add version
}

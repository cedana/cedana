package plugins

// Here, are defined the core functions to load plugins. These functions assume
// that the plugins are installed in the system by a manager.

// Functions here never error out, they just return empty values. If a plugin
// cannot be accessed, the feature that depends on it is just disabled.

import (
	"fmt"
	"os"
	"path/filepath"
	"plugin"
	"sync"
)

var Load = sync.OnceValue(loadPlugins)

const (
	LibDir = "/usr/local/lib"
	BinDir = "/usr/local/bin"
)

// LoadPlugins loads plugins from an installation directory.
func loadPlugins() (loadedPlugins map[string]*plugin.Plugin) {
	// look for plugins in the installation directory
	// and return a list of paths

	loadedPlugins = map[string]*plugin.Plugin{}

	if _, err := os.Stat(LibDir); os.IsNotExist(err) {
		return nil
	}

	for _, t := range Registry {
		if t.Type != Supported && t.Type != Experimental {
			continue
		}

		for _, file := range t.Libraries {
			path := filepath.Join(LibDir, file.Name)
			if _, err := os.Stat(path); os.IsNotExist(err) {
				continue
			}

			p, err := plugin.Open(path)
			if err != nil {
				fmt.Printf("Error loading plugin: %s\n", t.Name)
				continue
			}

			loadedPlugins[t.Name] = p
		}
	}

	return
}

// RecoverFromPanic is a helper function to recover from panics in plugins
// and log the error. It should only be used with defer.
func RecoverFromPanic(plugin string) {
	if r := recover(); r != nil {
		fmt.Printf("Plugin %s failed: %s\n", plugin, r)
	}
}

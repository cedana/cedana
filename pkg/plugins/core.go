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
	"strings"
	"sync"

	"github.com/cedana/cedana/pkg/config"
)

var Load = sync.OnceValue(loadPlugins)

var LibDir, BinDir string

func init() {
	LibDir = config.Global.Plugins.LibDir
	BinDir = config.Global.Plugins.BinDir
}

// LoadPlugins loads plugins from an installation directory.
func loadPlugins() (loadedPlugins map[string]*plugin.Plugin) {
	// look for plugins in the installation directory
	// and return a list of paths

	loadedPlugins = map[string]*plugin.Plugin{}

	if _, err := os.Stat(LibDir); os.IsNotExist(err) {
		return nil
	}

	if len(os.Args) > 0 && strings.HasPrefix(os.Args[1], "plugin") {
		// Skip loading plugins when running plugin management commands
		fmt.Println("Skipping plugin loading for plugin management command")
		return nil
	}

	for _, t := range Registry {
		if t.Type != SUPPORTED && t.Type != EXPERIMENTAL {
			continue
		}

		for _, file := range t.Libraries {
			path := filepath.Join(LibDir, file.Name)
			if s, err := os.Stat(path); os.IsNotExist(err) || s.IsDir() {
				continue
			}

			p, err := plugin.Open(path)
			if err != nil {
				fmt.Printf("%s: %v\n", t.Name, err)
				continue
			}

			loadedPlugins[t.Name] = p
		}
	}

	return loadedPlugins
}

// RecoverFromPanic is a helper function to recover from panics in plugins
// and log the error. It should only be used with defer.
func RecoverFromPanic(plugin string) {
	if r := recover(); r != nil {
		fmt.Printf("Plugin %s failed: %s\n", plugin, r)
	}
}

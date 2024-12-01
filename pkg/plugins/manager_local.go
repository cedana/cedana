package plugins

// Implements a local plugin manager that searches for installable plugins from local paths.

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cedana/cedana/pkg/style"
)

var searchPath = os.Getenv("CEDANA_PLUGINS_LOCAL_SEARCH_PATH")

type LocalManager struct {
	srcDir map[string]string // map of plugin name to source directory
}

func NewManagerLocal() *LocalManager {
	return &LocalManager{
		make(map[string]string),
	}
}

func (m *LocalManager) Get(name string) *Plugin {
	for _, p := range Registry {
		if p.Name == name {
			p.SyncInstalled()
			return &p
		}
	}

	return nil
}

func (m *LocalManager) IsInstalled(name string) bool {
	for _, p := range Registry {
		if p.Name == name {
			p.SyncInstalled()
			return p.Status == Installed
		}
	}

	return false
}

// List returns a list of plugins that are available.
// If statuses are provided, only plugins with those statuses are returned.
func (m *LocalManager) List(status ...Status) (list []Plugin, err error) {
	list = make([]Plugin, 0)

	set := make(map[Status]any)
	for _, s := range status {
		set[s] = nil
	}

	for _, p := range Registry {
		p.SyncInstalled()

		// search if plugin files available in search path
		found := 0
		dir := ""
		size := int64(0)
		files := append(p.Libraries, p.Binaries...)
		for _, file := range files {
			for _, path := range strings.Split(searchPath, ":") {
				var stat os.FileInfo
				if stat, err = os.Stat(filepath.Join(path, file)); err != nil {
					continue
				}
				dir = path
				found += 1
				size += stat.Size()
				break
			}
		}
		if found == len(files) {
			m.srcDir[p.Name] = dir
			p.LatestVersion = "local"
			if p.Status != Installed {
				p.Status = Available
			}
			if p.Size == 0 {
				p.Size = size
			}
		}

		if _, ok := set[p.Status]; len(set) > 0 && !ok {
			continue
		}

		// sort list by status
		sort.Slice(list, func(i, j int) bool {
			if list[i].Status == list[j].Status {
				return list[i].Type < list[j].Type
			}
			return list[i].Status > list[j].Status
		})

		list = append(list, p)
	}

	return list, nil
}

func (m *LocalManager) Install(names []string) (chan int, chan string, chan error) {
	installed := make(chan int)
	errs := make(chan error)
	msgs := make(chan string)

	go func() {
		defer close(installed)
		defer close(errs)
		defer close(msgs)

		list, err := m.List()
		if err != nil {
			errs <- fmt.Errorf("Failed to list plugins: %w", err)
			return
		}

		availableSet := make(map[string]*Plugin)
		for _, plugin := range list {
			availableSet[plugin.Name] = &plugin
		}

		for _, name := range names {
			var plugin *Plugin
			var ok bool
			if plugin, ok = availableSet[name]; !ok {
				errs <- fmt.Errorf("Plugin %s is not available", name)
				continue
			}

			if plugin.Status == Installed {
				msgs <- fmt.Sprintf("Updating %s...", name)
			} else {
				msgs <- fmt.Sprintf("Installing %s...", name)
			}

			// Copy the plugin files from the source directory to the installation directory
			srcDir := m.srcDir[name]
			var err error
			for _, file := range plugin.Libraries {
				src := filepath.Join(srcDir, file)
				dest := filepath.Join(LibDir, file)
				if _, e := os.Stat(src); os.IsNotExist(e) {
					err = fmt.Errorf("No local plugin found")
					break
				}
				os.Remove(dest)
				if e := os.Link(src, dest); e != nil {
					err = fmt.Errorf("Failed to install %s: %w", name, e)
					break
				}
			}
			for _, file := range plugin.Binaries {
				src := filepath.Join(srcDir, file)
				dest := filepath.Join(BinDir, file)
				if _, e := os.Stat(src); os.IsNotExist(e) {
					err = fmt.Errorf("No local plugin found")
					break
				}
				os.Remove(dest)
				if e := os.Link(src, dest); e != nil {
					err = fmt.Errorf("Failed to install %s: %w", name, e)
					break
				}
			}
			if err != nil {
				errs <- err
				continue
			}

			if plugin.Status == Installed {
				msgs <- style.WarningColor.Sprintf("Updated %s", name)
			} else {
				msgs <- style.PositiveColor.Sprintf("Installed %s", name)
			}
			installed <- 1
		}
	}()

	return installed, msgs, errs
}

func (m *LocalManager) Remove(names []string) (chan int, chan string, chan error) {
	removed := make(chan int)
	errs := make(chan error)
	msgs := make(chan string)

	go func() {
		defer close(removed)
		defer close(errs)
		defer close(msgs)

		list, err := m.List()
		if err != nil {
			errs <- fmt.Errorf("Failed to list plugins: %w", err)
			return
		}

		availableSet := make(map[string]*Plugin)
		for _, plugin := range list {
			availableSet[plugin.Name] = &plugin
		}

		for _, name := range names {
			var plugin *Plugin
			var ok bool
			if plugin, ok = availableSet[name]; !ok {
				errs <- fmt.Errorf("Plugin %s is not available", name)
				continue
			}
			if plugin.Status != Installed {
				errs <- fmt.Errorf("Plugin %s is not installed", name)
				continue
			}

			msgs <- fmt.Sprintf("Removing %s...", name)

			// Remove the plugin files from the installation directory
			for _, file := range plugin.Libraries {
				dest := filepath.Join(LibDir, file)
				if e := os.Remove(dest); e != nil {
					errs <- fmt.Errorf("Failed to remove %s: %w", name, e)
					break
				}
			}
			for _, file := range plugin.Binaries {
				dest := filepath.Join(BinDir, file)
				if e := os.Remove(dest); e != nil {
					errs <- fmt.Errorf("Failed to remove %s: %w", name, e)
					break
				}
			}

			msgs <- style.NegativeColor.Sprintf("Removed %s", name)
			removed <- 1
		}
	}()

	return removed, msgs, errs
}

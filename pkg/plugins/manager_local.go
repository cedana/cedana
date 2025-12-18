package plugins

// Implements a local plugin manager that searches for installable plugins from local paths.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cedana/cedana/pkg/style"
	"github.com/cedana/cedana/pkg/utils"
)

var searchPath = os.Getenv("CEDANA_PLUGINS_LOCAL_SEARCH_PATH")

type LocalManager struct {
	searchPath string
	srcDir     map[string]string // map of plugin name to source directory
}

func NewLocalManager() *LocalManager {
	wd, _ := os.Getwd()
	return &LocalManager{
		searchPath + ":" + wd, // add current working directory to search path
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

func (m *LocalManager) Getf(format string, a ...any) *Plugin {
	return m.Get(fmt.Sprintf(format, a...))
}

func (m *LocalManager) IsInstalled(name string) bool {
	for _, p := range Registry {
		if p.Name == name {
			p.SyncInstalled()
			return p.IsInstalled()
		}
	}

	return false
}

// List returns a list of plugins that are available.
// If filter is provided, only plugins with the specified names are returned.
func (m *LocalManager) List(latest bool, filter ...string) (list []Plugin, err error) {
	list = make([]Plugin, 0)

	set := make(map[string]bool)
	for _, name := range filter {
		nameOnly := strings.Split(strings.TrimSpace(name), "@")[0]
		set[nameOnly] = true
	}

	for _, p := range Registry {
		if p.Type == UNIMPLEMENTED {
			continue
		}
		if _, ok := set[p.Name]; len(set) > 0 && !ok {
			continue
		}

		p.SyncInstalled()

		if !latest {
			list = append(list, p)
			continue
		}

		// search if plugin files available in search path
		found := 0
		dir := ""
		size := int64(0)
		var plublishedAt time.Time
		files := append(p.Libraries, p.Binaries...)
		var totalSum strings.Builder
		for _, file := range files {
			for path := range strings.SplitSeq(m.searchPath, ":") {
				var stat os.FileInfo
				if stat, err = os.Stat(filepath.Join(path, file.Name)); err != nil || stat.IsDir() {
					continue
				}
				dir = path
				found += 1
				size += stat.Size()
				plublishedAt = stat.ModTime()
				sum, _ := utils.FileMD5Sum(filepath.Join(path, file.Name))
				totalSum.WriteString(sum)
				break
			}
		}

		if found == len(files) {
			m.srcDir[p.Name] = dir
			p.AvailableVersion = "local"
			switch p.Status {
			case INSTALLED, OUTDATED:
				if p.Checksum() != totalSum.String() {
					p.Status = OUTDATED
				} else {
					p.Status = INSTALLED
					p.Version = p.AvailableVersion
				}
			case UNKNOWN:
				p.Status = AVAILABLE
			}
			if p.Size == 0 {
				p.Size = size
			}
			p.PublishedAt = plublishedAt
		}

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

		list, err := m.List(true, names...)
		if err != nil {
			errs <- fmt.Errorf("Failed to list plugins: %w", err)
			return
		}

		availableSet := make(map[string]*Plugin)
		for _, plugin := range list {
			availableSet[plugin.Name] = &plugin
		}

		for _, name := range names {
			name := strings.TrimSpace(name)
			if name == "" {
				continue
			}
			name = strings.Split(name, "@")[0] // ignore version specifier if any

			var plugin *Plugin
			var ok bool
			if plugin, ok = availableSet[name]; !ok {
				errs <- fmt.Errorf("Plugin %s is not available", name)
				continue
			}

			if plugin.Status == INSTALLED {
				msgs <- fmt.Sprintf("Plugin %s is already installed", name)
				continue
			} else if plugin.Status == AVAILABLE {
				msgs <- fmt.Sprintf("Installing plugin %s...", name)
			} else {
				msgs <- fmt.Sprintf("Updating plugin %s...", name)
			}

			// Copy the plugin files from the source directory to the installation directory
			srcDir := m.srcDir[name]
			var err error
			for _, file := range plugin.Libraries {
				src := filepath.Join(srcDir, file.Name)
				var dest string
				if file.InstallDir != "" {
					os.MkdirAll(file.InstallDir, os.ModePerm)
					dest = filepath.Join(file.InstallDir, file.Name)
				} else {
					dest = filepath.Join(LibDir, file.Name)
				}
				if s, e := os.Stat(src); e != nil || os.IsNotExist(e) || s.IsDir() {
					err = fmt.Errorf("No local plugin found")
					break
				}
				os.Remove(dest)
				if e := utils.CopyFile(src, dest); e != nil {
					err = fmt.Errorf("Failed to install %s: %w", name, e)
					break
				}
			}
			for _, file := range plugin.Binaries {
				src := filepath.Join(srcDir, file.Name)
				var dest string
				if file.InstallDir != "" {
					os.MkdirAll(file.InstallDir, os.ModePerm)
					dest = filepath.Join(file.InstallDir, file.Name)
				} else {
					dest = filepath.Join(BinDir, file.Name)
				}
				if s, e := os.Stat(src); e != nil || os.IsNotExist(e) || s.IsDir() {
					err = fmt.Errorf("No local plugin found")
					break
				}
				os.Remove(dest)
				if e := utils.CopyFile(src, dest); e != nil {
					err = fmt.Errorf("Failed to install %s: %w", name, e)
					break
				}
			}
			if err != nil {
				errs <- err
				continue
			}

			if plugin.Status == AVAILABLE {
				msgs <- style.PositiveColors.Sprintf("Installed plugin %s", name)
			} else {
				msgs <- style.WarningColors.Sprintf("Updated plugin %s", name)
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

		list, err := m.List(false)
		if err != nil {
			errs <- fmt.Errorf("Failed to list plugins: %w", err)
			return
		}

		availableSet := make(map[string]*Plugin)
		for _, plugin := range list {
			availableSet[plugin.Name] = &plugin
		}

		for _, name := range names {
			name := strings.TrimSpace(name)
			if name == "" {
				continue
			}
			var plugin *Plugin
			var ok bool
			if plugin, ok = availableSet[name]; !ok {
				errs <- fmt.Errorf("Plugin %s is not available", name)
				continue
			}
			if plugin.Status != INSTALLED {
				errs <- fmt.Errorf("Plugin %s is not installed", name)
				continue
			}

			msgs <- fmt.Sprintf("Removing %s...", name)

			// Remove the plugin files from the installation directory
			for _, file := range plugin.Libraries {
				var dest string
				if file.InstallDir != "" {
					dest = filepath.Join(file.InstallDir, file.Name)
				} else {
					dest = filepath.Join(LibDir, file.Name)
				}
				if e := os.Remove(dest); e != nil {
					err = fmt.Errorf("Failed to remove %s: %w", name, e)
					break
				}
			}
			for _, file := range plugin.Binaries {
				var dest string
				if file.InstallDir != "" {
					dest = filepath.Join(file.InstallDir, file.Name)
				} else {
					dest = filepath.Join(BinDir, file.Name)
				}
				if e := os.Remove(dest); e != nil {
					err = fmt.Errorf("Failed to remove %s: %w", name, e)
					break
				}
			}
			if err != nil {
				errs <- err
				continue
			}

			msgs <- style.NegativeColors.Sprintf("Removed %s", name)
			removed <- 1
		}
	}()

	return removed, msgs, errs
}

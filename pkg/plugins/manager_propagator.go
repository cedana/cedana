package plugins

// Implements a plugin manager, that uses the Propagator service as a backend.
// Has embedded LocalManager, and only needs to override a few methods.

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/cedana/cedana/pkg/config"
	"github.com/cedana/cedana/pkg/style"
)

const (
	BINARY_PERMS       = 0o755
	LIBRARY_PERMS      = 0o644
	DOWNLOAD_DIR_PERMS = 0o755
)

type PropagatorManager struct {
	config.Connection
	client *http.Client

	compatibility string // used to fetch plugins compatible with this version
	downloadDir   string
	*LocalManager
}

func NewPropagatorManager(connection config.Connection, compatibility string) *PropagatorManager {
	downloadDir := filepath.Join(os.TempDir(), "cedana", "downloads")
	os.RemoveAll(downloadDir) // cleanup existing downloads
	os.MkdirAll(downloadDir, DOWNLOAD_DIR_PERMS)

	localManager := NewLocalManager()

	// Add the temp download directory to its search path

	return &PropagatorManager{
		connection,
		&http.Client{},
		compatibility,
		downloadDir,
		localManager,
	}
}

func (m *PropagatorManager) List(latest bool, filter ...string) ([]Plugin, error) {
	list, err := m.LocalManager.List(latest, filter...)
	if err != nil {
		return nil, err
	}

	if !latest {
		return list, nil
	}

	// Now fetch the latest versions from the propagator for only plugins
	// that were not available locally. We don't want to fetch latest
	// if the user is using locally built plugins.

	var names []string
	for _, p := range list {
		if p.LatestVersion != "local" {
			names = append(names, p.Name)
		}
	}

	if len(names) == 0 { // nothing to fetch
		return list, nil
	}

	url := fmt.Sprintf("%s/plugins?names=%s&compatibility=%s", m.URL, strings.Join(names, ","), m.compatibility)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err == nil {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", m.AuthToken))

		var resp *http.Response
		resp, err = m.client.Do(req)
		if err == nil {
			defer resp.Body.Close()

			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				if resp.StatusCode == http.StatusPartialContent {
					fmt.Println(style.WarningColors.Sprint(
						"Some plugins have no compatible versions available in the registry or locally.\n",
						"You may need to update Cedana. If this is a local build, you may ignore this warning.\n",
					))
				}

				onlineList := make([]Plugin, len(list))
				if err := json.NewDecoder(resp.Body).Decode(&onlineList); err != nil {
					return nil, err
				}

				for i := range list {
					for j := range onlineList {
						if list[i].Name == onlineList[j].Name {
							list[i].LatestVersion = onlineList[j].LatestVersion
							list[i].Size = onlineList[j].Size
							list[i].PublishedAt = onlineList[j].PublishedAt

							if list[i].Status == INSTALLED || list[i].Status == OUTDATED {
								if list[i].Checksum() != onlineList[j].Checksum() {
									list[i].Status = OUTDATED
								} else {
									list[i].Status = INSTALLED
								}
							} else if list[i].Status == UNKNOWN {
								list[i].Status = AVAILABLE
							}
						}
					}
				}
			} else {
				err = fmt.Errorf("%s", resp.Status)
			}
		}
	}

	if err != nil {
		fmt.Println(style.WarningColors.Sprintf("Using local list. Failed to connect to propagator registry: %v", err))
	}

	return list, nil
}

func (m *PropagatorManager) Install(names []string) (chan int, chan string, chan error) {
	installed := make(chan int)
	errs := make(chan error)
	msgs := make(chan string)

	list, err := m.List(true, names...)
	if err != nil {
		errs <- fmt.Errorf("Failed to list plugins: %w", err)
		return installed, msgs, errs
	}

	availableSet := make(map[string]*Plugin)
	for _, plugin := range list {
		if plugin.Status != UNKNOWN {
			availableSet[plugin.Name] = &plugin
		}
	}

	installList := make([]string, 0, len(names))

	wg := sync.WaitGroup{}

	go func() {
		defer close(installed)
		defer close(msgs)
		defer close(errs)

		for _, name := range names {
			if _, ok := availableSet[name]; !ok {
				errs <- fmt.Errorf("Plugin %s is not available", name)
				continue
			}

			if availableSet[name].Status == INSTALLED {
				msgs <- fmt.Sprintf("Latest version of %s is already installed", name)
				continue
			}

			installList = append(installList, name)

			// If locally built plugins are available, skip downloading
			if availableSet[name].LatestVersion == "local" {
				continue
			}

			wg.Add(1)
			go func() {
				defer wg.Done()
				plugin := availableSet[name]

				msgs <- fmt.Sprintf("Downloading plugin %s...", name)
				for _, binary := range plugin.Binaries {
					err := m.downloadBinary(binary.Name, plugin.LatestVersion, BINARY_PERMS)
					if err != nil {
						msgs <- err.Error()
						msgs <- fmt.Sprintf("Will try a local version of %s if available", name)
						return
					}
				}

				for _, library := range plugin.Libraries {
					err := m.downloadBinary(library.Name, plugin.LatestVersion, LIBRARY_PERMS)
					if err != nil {
						msgs <- err.Error()
						msgs <- fmt.Sprintf("Will try a local version of %s if available", name)
						return
					}
				}
				msgs <- fmt.Sprintf("Downloaded plugin %s", name)
			}()
		}

		wg.Wait()

		// Now call the local manager to install the plugins. Update its search path
		// so it can find the downloaded plugins.
		m.LocalManager.searchPath = fmt.Sprintf("%s:%s", m.LocalManager.searchPath, m.downloadDir)
		i, a, e := m.LocalManager.Install(installList)
		for {
			select {
			case val, ok := <-i:
				if !ok {
					i = nil
					break
				}
				installed <- val
			case val, ok := <-a:
				if !ok {
					a = nil
					break
				}
				msgs <- val
			case val, ok := <-e:
				if !ok {
					e = nil
					break
				}
				errs <- val
			}
			if i == nil && a == nil && e == nil {
				break
			}
		}
	}()

	return installed, msgs, errs
}

////////////////////////
//// Helper Methods ////
////////////////////////

func (m *PropagatorManager) downloadBinary(binary string, version string, perms os.FileMode) error {
	if version == "" {
		version = "latest"
	}
	url := fmt.Sprintf("%s/download/%s/%s", m.URL, binary, version)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("Failed to build request for %s: %v", binary, err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", m.AuthToken))

	resp, err := m.client.Do(req)
	if err != nil {
		return fmt.Errorf("Failed to download %s: %v", binary, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Failed to download %s: %s", binary, resp.Status)
	}

	// Save the file to the download directory
	path := filepath.Join(m.downloadDir, binary)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, perms)
	if err != nil {
		return fmt.Errorf("Failed to save %s: %v", binary, err)
	}
	defer file.Close()

	if _, err := io.Copy(file, resp.Body); err != nil {
		return fmt.Errorf("Failed to save %s: %v", binary, err)
	}

	return nil
}

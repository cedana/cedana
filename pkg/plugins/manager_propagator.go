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
)

const (
	BINARY_PERMS  = 0o755
	LIBRARY_PERMS = 0o644
)

type PropagatorManager struct {
	config.Connection
	client *http.Client

	downloadDir string
	*LocalManager
}

func NewPropagatorManager(connection config.Connection) *PropagatorManager {
	downloadDir := filepath.Join(os.TempDir(), "cedana", "downloads")
	os.MkdirAll(downloadDir, 0755)

	localManager := NewLocalManager()
	localManager.searchPath = fmt.Sprintf("%s:%s", localManager.searchPath, downloadDir)

	// Add the temp download directory to its search path

	return &PropagatorManager{
		connection,
		&http.Client{},
		downloadDir,
		localManager,
	}
}

func (m *PropagatorManager) List(status ...Status) ([]Plugin, error) {
	list, err := m.LocalManager.List(status...)
	if err != nil {
		return nil, err
	}

	// Now update this information using the propagator service

	names := make([]string, len(list))
	for i, p := range list {
		names[i] = p.Name
	}
	url := fmt.Sprintf("%s/plugins?names=%s", m.URL, strings.Join(names, ","))

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", m.AuthToken))

	resp, err := m.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to list plugins: %s", resp.Status)
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
			}
		}
	}

	return list, nil
}

func (m *PropagatorManager) Install(names []string) (chan int, chan string, chan error) {
	installed := make(chan int)
	errs := make(chan error)
	msgs := make(chan string)

	list, err := m.LocalManager.List()
	if err != nil {
		errs <- fmt.Errorf("Failed to list plugins: %w", err)
		return installed, msgs, errs
	}

	availableSet := make(map[string]*Plugin)
	for _, plugin := range list {
		availableSet[plugin.Name] = &plugin
	}

	downloaded := make([]string, 0)

	wg := sync.WaitGroup{}

	for _, name := range names {
		wg.Add(1)
		go func() {
			defer wg.Done()
			plugin := availableSet[name]

			msgs <- fmt.Sprintf("Downloading %s...", name)
			for _, binary := range plugin.Binaries {
				err := m.downloadBinary(binary, BINARY_PERMS)
				if err != nil {
					errs <- err
					return
				}
			}

			for _, library := range plugin.Libraries {
				err := m.downloadBinary(library, LIBRARY_PERMS)
				if err != nil {
					errs <- err
					return
				}
			}
			msgs <- fmt.Sprintf("Downloaded %s", name)

			downloaded = append(downloaded, name)
		}()
	}

	wg.Wait()

	// Now call the local manager to install the plugins
	i, a, e := m.LocalManager.Install(downloaded)
	go func() {
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
			close(installed)
			close(msgs)
			close(errs)
		}
	}()

	return installed, msgs, errs
}

////////////////////////
//// Helper Methods ////
////////////////////////

func (m *PropagatorManager) downloadBinary(binary string, perms os.FileMode) error {
	url := fmt.Sprintf("%s/plugins/%s", m.URL, binary)
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

package plugins

// Implements a plugin manager, that uses the Propagator service as a backend.
// Has embedded LocalManager, and only needs to override a few methods.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"

	propagatorsdk "github.com/cedana/cedana-propagator-sdk/go"
	v1 "github.com/cedana/cedana-propagator-sdk/go/v1"
	"github.com/cedana/cedana/pkg/config"
	"github.com/cedana/cedana/pkg/style"
	"github.com/microsoft/kiota-abstractions-go/serialization"
)

const (
	BINARY_PERMS       = 0o755
	LIBRARY_PERMS      = 0o644
	DOWNLOAD_DIR_PERMS = 0o755
)

type PropagatorManager struct {
	config.Connection
	client     *http.Client // only for binary downloads, which are streamed to disk
	propagator *v1.V1RequestBuilder

	compatibility string // used to fetch plugins compatible with this version
	builds        string // builds to look for (release, alpha)
	arch          string // architecture to look for (amd64, arm64)
	downloadDir   string
	*LocalManager
}

func NewPropagatorManager(connection config.Connection, compatibility string) *PropagatorManager {
	var downloadDir string
	downloadDir = filepath.Join(os.TempDir(), "cedana", "downloads")

	os.RemoveAll(downloadDir) // cleanup existing downloads
	os.MkdirAll(downloadDir, DOWNLOAD_DIR_PERMS)

	localManager := NewLocalManager()
	builds := config.Global.Plugins.Builds

	return &PropagatorManager{
		connection,
		&http.Client{},
		propagatorsdk.NewClient(connection.URL, connection.AuthToken).V1(),
		compatibility,
		builds,
		runtime.GOARCH,
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
	// that were not available locally. We don't want to fetch anything
	// if the user is using locally built plugins.

	var names []string
	if len(filter) == 0 {
		for _, p := range list {
			if p.AvailableVersion != "local" {
				names = append(names, p.Name)
			}
		}
	} else {
		for _, name := range filter {
			if slices.ContainsFunc(list, func(p Plugin) bool {
				nameOnly := name
				if strings.Contains(name, "@") {
					nameOnly = strings.Split(name, "@")[0]
				}
				return p.Name == nameOnly && p.AvailableVersion != "local"
			}) {
				names = append(names, name)
			}
		}
	}

	if len(names) == 0 { // nothing to fetch
		return list, nil
	}

	namesParam := strings.Join(names, ",")
	requestConfiguration := &v1.PluginsRequestBuilderGetRequestConfiguration{
		QueryParameters: &v1.PluginsRequestBuilderGetQueryParameters{
			Names:         &namesParam,
			Build:         &m.builds,
			Compatibility: &m.compatibility,
			Arch:          &m.arch,
		},
	}

	res, err := m.propagator.Plugins().Get(context.Background(), requestConfiguration)
	if err != nil {
		fmt.Println(style.WarningColors.Sprintf("Using local list. Failed to connect to propagator registry: %v", err))
		return list, nil
	}

	onlineList := make([]Plugin, len(res))
	for i, model := range res {
		if err := fromModel(model, &onlineList[i]); err != nil {
			return nil, err
		}
	}

	// The registry omits plugins with no compatible version (previously
	// signaled with a 206 status, which the SDK does not expose).
	if len(onlineList) < len(names) {
		fmt.Println(style.WarningColors.Sprint("Some requested plugins have no compatible versions available in the registry or locally.\n"))
	}

	for i := range list {
		for j := range onlineList {
			if list[i].Name == onlineList[j].Name {
				list[i].AvailableVersion = onlineList[j].AvailableVersion
				list[i].Size = onlineList[j].Size
				list[i].PublishedAt = onlineList[j].PublishedAt

				switch list[i].Status {
				case INSTALLED, OUTDATED:
					if list[i].Checksum() != onlineList[j].Checksum() {
						list[i].Status = OUTDATED
					} else {
						list[i].Status = INSTALLED
					}
				case UNKNOWN:
					list[i].Status = AVAILABLE
				}
			}
		}
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
			name := strings.Split(name, "@")[0]

			if _, ok := availableSet[name]; !ok {
				errs <- fmt.Errorf("Plugin %s is not available", name)
				continue
			}

			if availableSet[name].Status == INSTALLED {
				msgs <- fmt.Sprintf("Plugin %s is already installed", name)
				continue
			}

			installList = append(installList, name)

			// If locally built plugins are available, skip downloading
			if availableSet[name].AvailableVersion == "local" {
				continue
			}

			wg.Go(func() {
				plugin := availableSet[name]

				msgs <- fmt.Sprintf("Downloading plugin %s (%s)...", name, plugin.AvailableVersion)
				for _, binary := range plugin.Binaries {
					err := m.downloadBinary(binary.Name, plugin.AvailableVersion, m.arch, m.builds, BINARY_PERMS)
					if err != nil {
						msgs <- err.Error()
						msgs <- fmt.Sprintf("Will try a local version of %s if available", name)
						return
					}
				}

				for _, library := range plugin.Libraries {
					err := m.downloadBinary(library.Name, plugin.AvailableVersion, m.arch, m.builds, LIBRARY_PERMS)
					if err != nil {
						msgs <- err.Error()
						msgs <- fmt.Sprintf("Will try a local version of %s if available", name)
						return
					}
				}
				msgs <- fmt.Sprintf("Downloaded plugin %s (%s)", name, plugin.AvailableVersion)
			})
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

// downloadBinary uses a raw HTTP client instead of the SDK, as the endpoint
// redirects to a presigned S3 URL and the binary must be streamed to disk.
func (m *PropagatorManager) downloadBinary(binary string, version string, arch string, build string, perms os.FileMode) error {
	if version == "" {
		version = "latest"
	}

	url := fmt.Sprintf("%s/plugins/download/%s?version=%s&arch=%s&build=%s", m.URL, binary, version, arch, build)
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

// fromModel converts an SDK model back into a JSON-deserializable value.
func fromModel(model serialization.Parsable, out any) error {
	data, err := serialization.Serialize("application/json", model)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

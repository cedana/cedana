package runc

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cedana/runc/libcontainer"
	rspec "github.com/opencontainers/runtime-spec/specs-go"
)

func List(root string) error {
	dir, err := os.Open(root)
	if err != nil {
		return err
	}
	defer dir.Close()
	fileInfos, err := dir.Readdir(-1)
	if err != nil {
		return err
	}

	var runcContainers []string // Slice to hold directory names

	for _, fileInfo := range fileInfos {
		if fileInfo.IsDir() {
			runcContainers = append(runcContainers, fileInfo.Name())
			fmt.Println(fileInfo.Name())
		}
	}
	return nil
}

func GetContainerIdByName(containerName string, root string) (string, error) {
	dirs, err := os.ReadDir(root)
	if err != nil {
		return "", err
	}
	for _, dir := range dirs {
		var spec rspec.Spec
		var runcSpec libcontainer.State
		var bundle string
		if dir.IsDir() {
			statePath := filepath.Join(root, dir.Name(), "state.json")
			if _, err := os.Stat(statePath); err == nil {
				configFile, err := os.ReadFile(statePath)
				if err != nil {
					return "", err
				}
				if err := json.Unmarshal(configFile, &runcSpec); err != nil {
					return "", err
				}
				for _, label := range runcSpec.Config.Labels {
					splitLabel := strings.Split(label, "=")
					if splitLabel[0] == "bundle" {
						bundle = splitLabel[1]
					}
				}
			}
		}

		configPath := filepath.Join("host", bundle, "config.json")
		if _, err := os.Stat(configPath); err == nil {
			configFile, err := os.ReadFile(configPath)
			if err != nil {
				return "", err
			}
			if err := json.Unmarshal(configFile, &spec); err != nil {
				return "", err
			}
			if spec.Annotations["io.kubernetes.cri.container-name"] == containerName {
				return dir.Name(), nil
			}
		}

	}
	return "", fmt.Errorf("Container id not found")
}

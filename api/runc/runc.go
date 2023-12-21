package runc

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

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
		if dir.IsDir() {
			configPath := filepath.Join(root, dir.Name(), "config.json")
			if _, err := os.Stat(configPath); err == nil {
				configFile, err := os.ReadFile(configPath)
				if err != nil {
					return "", err
				}
				if err := json.Unmarshal(configFile, &spec); err != nil {
					return "", err
				}
			}
		}
		if spec.Annotations["io.kubernetes.cri.container-name"] == containerName {
			return dir.Name(), nil
		}
	}
	return "", fmt.Errorf("Container id not found")
}

package kube

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/cedana/runc/libcontainer"
	rspec "github.com/opencontainers/runtime-spec/specs-go"
)

// Kube default sandbox annotation keys
const (
	CONTAINER_TYPE    = "io.kubernetes.cri.container-type"
	SANDBOX_ID        = "io.kubernetes.cri.sandbox-id"
	SANDBOX_NAME      = "io.kubernetes.cri.sandbox-name"
	SANDBOX_NAMESPACE = "io.kubernetes.cri.sandbox-namespace"
	SANDBOX_UID       = "io.kubernetes.cri.sandbox-uid"
	LOG_DIRECTORY     = "io.kubernetes.cri.sandbox-log-directory"

	// Kube container only annotation keys
	CONTAINER_NAME = "io.kubernetes.cri.container-name"
	IMAGE_NAME     = "io.kubernetes.cri.image-name"
)

const CONTAINER_TYPE_CONTAINER = "container"

const CONTAINER_TYPE_SANDBOX = "sandbox"

type Container struct {
	containerName string
	sandboxId     string
}

type RuncContainer struct {
	ContainerId string
	Annotations map[string]string
}

func StateList(root string) ([]map[string]string, error) {
	var ContainerAnnotations []map[string]string

	dirs, err := os.ReadDir(root)
	if err != nil {
		return nil, err
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
					return nil, err
				}
				if err := json.Unmarshal(configFile, &runcSpec); err != nil {
					return nil, err
				}
				for _, label := range runcSpec.Config.Labels {
					splitLabel := strings.Split(label, "=")
					if splitLabel[0] == "bundle" {
						bundle = splitLabel[1]
					}
				}
			}
		}

		configPath := filepath.Join(bundle, "config.json")
		if _, err := os.Stat(configPath); err == nil {
			configFile, err := os.ReadFile(configPath)
			if err != nil {
				return nil, err
			}
			if err := json.Unmarshal(configFile, &spec); err != nil {
				return nil, err
			}
		}
		ContainerAnnotations = append(ContainerAnnotations, spec.Annotations)
	}

	return ContainerAnnotations, nil
}

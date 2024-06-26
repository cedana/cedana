package runc

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/cedana/cedana/api/kube"
	"github.com/cedana/runc/libcontainer"
	rspec "github.com/opencontainers/runtime-spec/specs-go"
)

type runcContainer struct {
	ContainerId      string
	Bundle           string
	ContainerName    string
	ImageName        string
	SandboxId        string
	SandboxName      string
	SandboxNamespace string
	SandboxUid       string
}

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
		}
	}
	return nil
}

func GetPidByContainerId(containerId, root string) (int32, error) {
	statePath := filepath.Join(root, containerId, "state.json")
	var runcSpec libcontainer.State
	if _, err := os.Stat(statePath); err == nil {
		configFile, err := os.ReadFile(statePath)
		if err != nil {
			return 0, err
		}
		if err := json.Unmarshal(configFile, &runcSpec); err != nil {
			return 0, err
		}
	}
	return int32(runcSpec.InitProcessPid), nil
}

func RuncGetAll(root, namespace string) ([]runcContainer, error) {
	var containers []runcContainer
	kubeContainers, err := kube.StateList(root)
	if err != nil {
		return containers, err
	}

	for _, sandbox := range kubeContainers {
		var c runcContainer

		if sandbox.Annotations[kube.CONTAINER_TYPE] == kube.CONTAINER_TYPE_CONTAINER {
			c.ContainerName = sandbox.Annotations[kube.CONTAINER_NAME]
			c.ImageName = sandbox.Annotations[kube.IMAGE_NAME]
			c.SandboxId = sandbox.Annotations[kube.SANDBOX_ID]
			c.SandboxName = sandbox.Annotations[kube.SANDBOX_NAME]
			c.SandboxUid = sandbox.Annotations[kube.SANDBOX_UID]
			c.SandboxNamespace = sandbox.Annotations[kube.SANDBOX_NAMESPACE]
			c.ContainerId = sandbox.ContainerId
			c.Bundle = sandbox.Bundle

			if sandbox.Annotations[kube.SANDBOX_NAMESPACE] == namespace || namespace == "" && c.ImageName != "" {
				containers = append(containers, c)
			}
		}
	}

	return containers, nil
}

func GetContainerIdByName(containerName, sandboxName, root string) (string, string, error) {
	dirs, err := os.ReadDir(root)
	if err != nil {
		return "", "", err
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
					return "", "", err
				}
				if err := json.Unmarshal(configFile, &runcSpec); err != nil {
					return "", "", err
				}
				for _, label := range runcSpec.Config.Labels {
					splitLabel := strings.Split(label, "=")
					if splitLabel[0] == "bundle" {
						bundle = splitLabel[1]
					}
				}
			}
		}

		// TODO the prefixing here is for k3s only, or !cedana-helper. Meaning that Cedana is running inside the Cedana Daemonset deployment and not on host
		configPath := filepath.Join(bundle, "config.json")
		if _, err := os.Stat(configPath); err == nil {
			configFile, err := os.ReadFile(configPath)
			if err != nil {
				return "", "", err
			}
			if err := json.Unmarshal(configFile, &spec); err != nil {
				return "", "", err
			}

			if sandboxName != "" {
				if spec.Annotations["io.kubernetes.cri.container-name"] == containerName && spec.Annotations["io.kubernetes.cri.sandbox-name"] == sandboxName {
					return dir.Name(), bundle, nil
				}
			} else {
				if spec.Annotations["io.kubernetes.cri.container-name"] == containerName {
					return dir.Name(), bundle, nil
				}
			}
		}

	}
	return "", "", fmt.Errorf("Container id not found")
}

func GetPausePid(bundlePath string) (int, error) {
	var spec rspec.Spec
	var pid int

	configPath := filepath.Join(bundlePath, "config.json")
	if _, err := os.Stat(configPath); err == nil {
		configFile, err := os.ReadFile(configPath)
		if err != nil {
			return 0, err
		}
		if err := json.Unmarshal(configFile, &spec); err != nil {
			return 0, err
		}
		for _, ns := range spec.Linux.Namespaces {
			if ns.Type == "network" {
				path := ns.Path
				splitPath := strings.Split(path, "/")
				pid, err = strconv.Atoi(splitPath[2])
				if err != nil {
					return 0, err
				}
				break
			}
		}
	}

	return pid, nil
}

func GetSpecById(root, containerID string) (spec *rspec.Spec, err error) {

	configFile, err := os.ReadFile(filepath.Join(root, containerID))
	if err != nil {
		return spec, err
	}
	if err := json.Unmarshal(configFile, &spec); err != nil {
		return spec, err
	}

	return spec, err
}

package kube

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cedana/cedana/plugins/runc/pkg/runc"
	securejoin "github.com/cyphar/filepath-securejoin"
	"github.com/opencontainers/runc/libcontainer"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/spf13/afero"
)

// Kube default sandbox annotation keys
const (
	// CRI-O
	CRIO_CONTAINER_TYPE    = "io.kubernetes.cri-o.ContainerType"
	CRIO_IMAGE_NAME        = "io.kubernetes.cri-o.ImageName"
	CRIO_SANDBOX_NAMESPACE = "io.kubernetes.pod.namespace"
	CRIO_SANDBOX_NAME      = "io.kubernetes.pod.name"
	CRIO_CONTAINER_NAME    = "io.kubernetes.container.name"
	CRIO_SANDBOX_ID        = "io.kubernetes.cri-o.SandboxID"
	CRIO_LOG_DIRECTORY     = "io.kubernetes.cri-o.LogPath"

	// CRI
	CONTAINER_TYPE    = "io.kubernetes.cri.container-type"
	SANDBOX_ID        = "io.kubernetes.cri.sandbox-id"
	SANDBOX_NAME      = "io.kubernetes.cri.sandbox-name"
	SANDBOX_NAMESPACE = "io.kubernetes.cri.sandbox-namespace"
	SANDBOX_UID       = "io.kubernetes.cri.sandbox-uid"
	LOG_DIRECTORY     = "io.kubernetes.cri.sandbox-log-directory"

	// Kube container only annotation keys
	CONTAINER_NAME     = "io.kubernetes.cri.container-name"
	IMAGE_NAME         = "io.kubernetes.cri.image-name"
	SANDBOX_IMAGE_NAME = "io.kubernetes.cri.podsandbox.image-name"
)

const (
	CONTAINER_TYPE_CONTAINER = "container"
	CONTAINER_TYPE_SANDBOX   = "sandbox"
)

var RootNameAnnotationMap = map[string][2]string{
	"/run/runc":     {CRIO_CONTAINER_NAME, CRIO_SANDBOX_NAME},
	"/var/run/runc": {CRIO_CONTAINER_NAME, CRIO_SANDBOX_NAME},
}

type Container struct {
	ID               string
	Name             string
	Bundle           string
	Type             string
	Annotations      map[string]string
	Image            string
	SandboxID        string
	SandboxName      string
	SandboxNamespace string
	SandboxUID       string
}

type KubeClient interface {
	ListContainers(fs afero.Fs, root, namespace string) ([]*Container, error)
}

type DefaultKubeClient struct{}

func (c *DefaultKubeClient) ListContainers(fs afero.Fs, root, namespace string, containerType ...string) ([]*Container, error) {
	var containers []*Container

	entries, err := afero.ReadDir(fs, root)
	if err != nil {
		return nil, fmt.Errorf("failed to list root directory: %v", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		id := entry.Name()
		var spec *specs.Spec
		var state *libcontainer.State
		var bundle string

		stateDir, err := securejoin.SecureJoin(root, id)
		if err != nil {
			return nil, err
		}

		_, err = os.Stat(filepath.Join(stateDir, "state.json"))
		if err != nil {
			return nil, fmt.Errorf("failed to stat state.json: %v", err)
		}

		ctr, err := libcontainer.Load(root, id)
		if err != nil {
			return nil, fmt.Errorf("failed to load container %s on root %s statedir %s: %v", id, root, stateDir, err)
		}
		state, err = ctr.State()
		if err != nil {
			return nil, err
		}

		for _, label := range state.Config.Labels {
			if strings.HasPrefix(label, "bundle") {
				bundle = strings.Split(label, "=")[1]
				break
			}
		}
		if bundle == "" {
			return nil, fmt.Errorf("failed to get bundle from state config: %v", state.Config.Labels)
		}

		spec, err = runc.LoadSpec(filepath.Join(bundle, "config.json"))
		if err != nil {
			// If we can't load the spec, skip this container
			// This happens when cedana messes up and leaves a stale container behind
			continue
		}

		var containerNameAnnotation, sandboxNameAnnotation string
		if val, ok := RootNameAnnotationMap[root]; ok {
			containerNameAnnotation, sandboxNameAnnotation = val[0], val[1]
		} else {
			containerNameAnnotation, sandboxNameAnnotation = CONTAINER_NAME, SANDBOX_NAME
		}

		container := Container{
			ID:          id,
			Bundle:      bundle,
			Annotations: spec.Annotations,
		}

		if len(containerType) == 0 || spec.Annotations[CONTAINER_TYPE] == containerType[0] || spec.Annotations[CRIO_CONTAINER_TYPE] == containerType[0] {
			container.Type = getFirstNonEmptyAnnotation(spec.Annotations, CONTAINER_TYPE, CRIO_CONTAINER_TYPE)
			container.Name = spec.Annotations[containerNameAnnotation]
			container.Image = getFirstNonEmptyAnnotation(spec.Annotations, IMAGE_NAME, CRIO_IMAGE_NAME, SANDBOX_IMAGE_NAME)
			container.SandboxID = getFirstNonEmptyAnnotation(spec.Annotations, SANDBOX_ID, CRIO_SANDBOX_ID)
			container.SandboxName = spec.Annotations[sandboxNameAnnotation]
			container.SandboxUID = spec.Annotations[SANDBOX_UID]
			container.SandboxNamespace = getFirstNonEmptyAnnotation(spec.Annotations, SANDBOX_NAMESPACE, CRIO_SANDBOX_NAMESPACE)

			if (namespace == "" || container.SandboxNamespace == namespace) && container.Image != "" {
				containers = append(containers, &container)
			}
		}
	}
	return containers, nil
}

func getFirstNonEmptyAnnotation(annotations map[string]string, keys ...string) string {
	for _, key := range keys {
		if val, exists := annotations[key]; exists && val != "" {
			return val
		}
	}
	return ""
}

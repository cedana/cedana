package kube

import (
	"github.com/opencontainers/runtime-spec/specs-go"
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

type PodInfo struct {
	ID          string
	Name        string
	Namespace   string
	UID         string
	Image       string
	Env         []string
	Annotations map[string]string
	Type        string
}

// PodInfoFromRunc extracts PodInfo from a container spec
func PodInfoFromRunc(spec *specs.Spec) (*PodInfo, error) {
	info := &PodInfo{
		Env:         spec.Process.Env,
		Annotations: spec.Annotations,
	}

	info.ID = GetFirstNonEmptyAnnotation(spec.Annotations, SANDBOX_ID, CRIO_SANDBOX_ID)
	info.Name = GetFirstNonEmptyAnnotation(spec.Annotations, SANDBOX_NAME, CRIO_SANDBOX_NAME)
	info.Namespace = GetFirstNonEmptyAnnotation(spec.Annotations, SANDBOX_NAMESPACE, CRIO_SANDBOX_NAMESPACE)
	info.UID = spec.Annotations[SANDBOX_UID]
	info.Type = GetFirstNonEmptyAnnotation(spec.Annotations, CONTAINER_TYPE, CRIO_CONTAINER_TYPE)
	info.Image = GetFirstNonEmptyAnnotation(spec.Annotations, IMAGE_NAME, CRIO_IMAGE_NAME, SANDBOX_IMAGE_NAME)

	return info, nil
}

func GetFirstNonEmptyAnnotation(annotations map[string]string, keys ...string) string {
	for _, key := range keys {
		if val, exists := annotations[key]; exists && val != "" {
			return val
		}
	}
	return ""
}

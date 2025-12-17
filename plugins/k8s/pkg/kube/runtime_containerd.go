package kube

import (
	"context"
	"fmt"
	"path/filepath"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/plugins/containerd"
	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/plugins/k8s"
	"github.com/cedana/cedana/pkg/features"
	"github.com/cedana/cedana/pkg/types"
	containerd_utils "github.com/cedana/cedana/plugins/containerd/pkg/utils"
	"github.com/cedana/cedana/plugins/k8s/internal/defaults"
	runc_utils "github.com/cedana/cedana/plugins/runc/pkg/runc"
)

// Implements the K8s runtime client interface for Containerd
type ContainerdClient struct{}

func NewContainerdClient() (*ContainerdClient, error) {
	return &ContainerdClient{}, nil
}

func (c *ContainerdClient) String() string {
	return "containerd"
}

func (c *ContainerdClient) Pods(ctx context.Context, req *k8s.QueryReq) (resp *k8s.QueryResp, err error) {
	var query types.Query

	err = features.QueryHandler.IfAvailable(func(_ string, containerQuery types.Query) error {
		query = containerQuery
		return nil
	}, "containerd")
	if err != nil {
		return nil, fmt.Errorf("failed to get containerd query handler: %v", err)
	}

	containerResps, err := query(ctx, &daemon.QueryReq{
		Containerd: &containerd.QueryReq{
			Namespace: defaults.CONTAINERD_NAMESPACE,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to query containerd: %v", err)
	}

	var info *PodInfo
	pods := []*k8s.Pod{}
	podMap := make(map[string]*k8s.Pod)
	namesMap := make(map[string]bool)
	for _, name := range req.Names {
		namesMap[name] = true
	}

	for _, container := range containerResps.Containerd.Containers {
		runtime := containerd_utils.Runtime(container)

		switch runtime {
		case "runc":
			runc := container.Runc
			if runc == nil {
				return nil, fmt.Errorf("container %s is missing lower runtime details", container.ID)
			}
			spec, err := runc_utils.LoadSpec(filepath.Join(runc.Bundle, runc_utils.SpecConfigFile))
			if err != nil {
				return nil, fmt.Errorf("failed to load runc spec for container %s: %v", container.ID, err)
			}
			info, err = PodInfoFromRunc(spec)
			if err != nil {
				return nil, fmt.Errorf("failed to get pod info from runc container %s: %v", container.ID, err)
			}
		default:
			return nil, fmt.Errorf("unsupported lower runtime: %s", runtime)
		}

		if len(namesMap) > 0 {
			if _, ok := namesMap[info.Name]; !ok {
				continue
			}
		}
		if req.Namespace != "" && info.Namespace != req.Namespace {
			continue
		}
		if req.ContainerType != "" && req.ContainerType != info.Type {
			continue
		}

		pod := podMap[info.ID]
		if pod == nil {
			pod = &k8s.Pod{
				ID:        info.ID,
				Name:      info.Name,
				Namespace: info.Namespace,
				UID:       info.UID,
			}
		}

		pod.Containerd = append(pod.Containerd, container)
	}

	for _, pod := range podMap {
		pods = append(pods, pod)
	}

	return &k8s.QueryResp{Pods: pods}, nil
}

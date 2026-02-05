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

func (c *ContainerdClient) Query(ctx context.Context, req *daemon.QueryReq) (resp *daemon.QueryResp, err error) {
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
	for _, name := range req.K8S.Names {
		namesMap[name] = true
	}

	resp = &daemon.QueryResp{K8S: &k8s.QueryResp{}, Messages: containerResps.Messages, States: containerResps.States}

	for _, container := range containerResps.Containerd.Containers {
		runtime := containerd_utils.Runtime(container)

		switch runtime {
		case "runc":
			runc := container.Runc
			if runc == nil {
				resp.Messages = append(resp.Messages, fmt.Sprintf("%s: missing runc details", container.ID))
				continue
			}
			spec, err := runc_utils.LoadSpec(filepath.Join(runc.Bundle, runc_utils.SpecConfigFile))
			if err != nil {
				resp.Messages = append(resp.Messages, fmt.Sprintf("%s: failed to load runc spec: %v", container.ID, err))
				continue
			}
			info, err = PodInfoFromRunc(spec)
			if err != nil {
				resp.Messages = append(resp.Messages, fmt.Sprintf("%s: failed to get pod info from runc container: %v", container.ID, err))
				continue
			}
		case "unsupported":
			resp.Messages = append(resp.Messages, fmt.Sprintf("%s: unsupported lower runtime", container.ID))
			continue
		default:
			resp.Messages = append(resp.Messages, fmt.Sprintf("%s: unsupported lower runtime `%s`", container.ID, runtime))
			continue
		}

		if len(namesMap) > 0 {
			if _, ok := namesMap[info.Name]; !ok {
				continue
			}
		}
		if req.K8S.Namespace != "" && req.K8S.Namespace != info.Namespace {
			continue
		}
		if req.K8S.ContainerType != "" && req.K8S.ContainerType != info.Type {
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
			podMap[info.ID] = pod
		}

		pod.Containerd = append(pod.Containerd, container)
	}

	for _, pod := range podMap {
		pods = append(pods, pod)
	}

	resp.K8S.Pods = pods

	return resp, nil
}

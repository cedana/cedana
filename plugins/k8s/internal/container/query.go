package container

import (
	"context"
	"fmt"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/plugins/containerd"
	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/plugins/k8s"
	"github.com/cedana/cedana/plugins/containerd/pkg/utils"
	"github.com/cedana/cedana/plugins/k8s/pkg/kube"
	"github.com/spf13/afero"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const DEFAULT_CONTAINERD_ROOT = "/run/containerd/runc/k8s.io"

// Implements the query handler for k8s
type QueryHandler interface {
	Query(ctx context.Context, req *daemon.QueryReq) (*daemon.QueryResp, error)
}

type DefaultQueryHandler struct {
	afero.Fs
}

func (h *DefaultQueryHandler) Query(ctx context.Context, req *daemon.QueryReq) (*daemon.QueryResp, error) {
	query := req.K8S

	if query == nil {
		return nil, status.Errorf(codes.InvalidArgument, "k8s query missing")
	}
	if query.Root == "" {
		query.Root = DEFAULT_CONTAINERD_ROOT
	}

	resp := &daemon.QueryResp{K8S: &k8s.QueryResp{}}
	kubeClient := &kube.DefaultKubeClient{}
	containerdNamespace := utils.NamespaceFromRoot(query.Root)

	fs := afero.NewOsFs()
	containers, err := kubeClient.ListContainers(fs, query.Root, query.Namespace, query.ContainerType)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list k8s containers: %v", err)
	}

	skipEmptyNames := true
	sandboxNameSet := make(map[string]bool)
	for _, name := range query.Names {
		skipEmptyNames = false
		sandboxNameSet[name] = true
	}

	podMap := make(map[string]*k8s.Pod)

	for _, container := range containers {
		if skipEmptyNames || sandboxNameSet[container.SandboxName] {
			pod, ok := podMap[container.SandboxID]
			if !ok {
				pod = &k8s.Pod{
					ID:        container.SandboxID,
					Name:      container.SandboxName,
					Namespace: container.SandboxNamespace,
					UID:       container.SandboxUID,
				}
				podMap[container.SandboxID] = pod
			}
			pod.Containerd = append(pod.Containerd, &containerd.Containerd{
				ID:        container.ID,
				Image:     &containerd.Image{Name: container.Image},
				Namespace: containerdNamespace,
			})
		}
	}

	for _, pod := range podMap {
		resp.K8S.Pods = append(resp.K8S.Pods, pod)
	}

	if len(resp.K8S.Pods) == 0 {
		return nil, status.Errorf(codes.NotFound, "no pods found")
	}

	resp.Messages = append(resp.Messages, fmt.Sprintf("Found %d pod(s)", len(resp.K8S.Pods)))

	return resp, nil
}

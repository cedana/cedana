package container

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/plugins/k8s"
	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/plugins/runc"
	"github.com/cedana/cedana/plugins/k8s/pkg/kube"
	"github.com/spf13/afero"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

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
		return nil, status.Errorf(codes.InvalidArgument, "k8s root missing")
	}
	if query.Namespace == "" {
		return nil, status.Errorf(codes.InvalidArgument, "k8s namespace missing")
	}

	resp := &daemon.QueryResp{K8S: &k8s.QueryResp{}}
	kubeClient := &kube.DefaultKubeClient{}

	fs := afero.NewOsFs()
	containers, err := kubeClient.ListContainers(fs, query.Root, query.Namespace)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list k8s containers: %v", err)
	}

	skipContainerNameMatch := len(query.ContainerNames) == 0
	skipSandboxNameMatch := 0 == len(query.SandboxNames)
	containerNameSet := make(map[string]bool)
	sandboxNameSet := make(map[string]bool)
	for _, name := range query.ContainerNames {
		containerNameSet[name] = true
	}
	for _, name := range query.SandboxNames {
		sandboxNameSet[name] = true
	}
	for _, container := range containers {
		if (skipContainerNameMatch || containerNameSet[container.Name]) &&
			(skipSandboxNameMatch || sandboxNameSet[container.SandboxName]) {
			resp.K8S.Containers = append(resp.K8S.Containers, &k8s.Container{
				SandboxID:        container.SandboxID,
				SandboxName:      container.SandboxName,
				SandboxNamespace: container.SandboxNamespace,
				SandboxUID:       container.SandboxUID,
				Image:            container.Image,
				Name:             container.Name,

				Runc: &runc.Runc{
					ID:     container.ID,
					Bundle: container.Bundle,
					Root:   query.Root,
				},
			})
		}
	}

	return resp, nil
}

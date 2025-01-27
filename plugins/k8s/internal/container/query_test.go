package container_test

import (
	"context"
	"testing"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/plugins/k8s"
	"github.com/cedana/cedana/plugins/k8s/internal/container"
	"github.com/cedana/cedana/plugins/k8s/pkg/kube"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// Mock kube.ListContainers
type MockKubeClient struct {
	mock.Mock
}

func (m *MockKubeClient) ListContainers(root, namespace string) ([]*kube.Container, error) {
	args := m.Called(root, namespace)
	return args.Get(0).([]*kube.Container), args.Error(1)
}

func TestQuery(t *testing.T) {
	mockKubeClient := &MockKubeClient{}

	mockKubeClient.On("ListContainers", "/test-root", "default").Return([]*kube.Container{
		{
			Name:             "container1",
			SandboxName:      "sandbox1",
			SandboxID:        "sandbox-id-1",
			SandboxUID:       "uid-1",
			SandboxNamespace: "default",
			Image:            "image1",
			ID:               "container-id-1",
			Bundle:           "/bundle",
		},
	}, nil)

	ctx := context.Background()
	req := &daemon.QueryReq{
		K8S: &k8s.QueryReq{
			Root:           "/test-root",
			Namespace:      "default",
			ContainerNames: []string{"container1"},
			SandboxNames:   []string{"sandbox1"},
		},
	}

	resp, err := container.Query(ctx, req)

	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Len(t, resp.K8S.Containers, 1)
	assert.Equal(t, "container-id-1", resp.K8S.Containers[0].Runc.ID)
	assert.Equal(t, "image1", resp.K8S.Containers[0].Image)

	mockKubeClient.AssertExpectations(t)
}

package containerd

import (
	"github.com/containerd/containerd"
)

type ContainerdService struct {
	client *containerd.Client
}

func New(address string) (*ContainerdService, error) {

	client, err := containerd.New(address)

	if err != nil {
		return nil, err
	}

	return &ContainerdService{client}, nil
}

func (*ContainerdService) DumpRootfs() (string, error) {

	return "NOT IMPLEMENTED", nil
}

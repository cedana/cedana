package containerd

import (
	"github.com/containerd/containerd"
	"github.com/urfave/cli"
)

type ContainerdService struct {
	client *containerd.Client
}

func New(context *cli.Context) (*ContainerdService, error) {

	timeoutOpt := containerd.WithTimeout(context.Duration("connect-timeout"))
	opts = append(opts, timeoutOpt)
	client, err := containerd.New(context.String("address"), opts...)
	if err != nil {
		return nil, err
	}

	return &ContainerdService{client}, nil
}

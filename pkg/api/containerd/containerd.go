package containerd

import (
	"github.com/cedana/cedana/pkg/api/services/task"
	"github.com/cedana/cedana/pkg/container"
	"github.com/containerd/containerd"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/errdefs"
	"golang.org/x/net/context"
)

type ContainerdService struct {
	client *containerd.Client
}

//func NewClient(context *cli.Context, opts ...containerd.Opt) (*containerd.Client, gocontext.Context, gocontext.CancelFunc, error) {
//timeoutOpt := containerd.WithTimeout(context.Duration("connect-timeout"))
//opts = append(opts, timeoutOpt)
//kclient, err := containerd.New(context.String("address"), opts...)

func New(ctx context.Context, address string) (*ContainerdService, error) {
	client, err := containerd.New(address)

	if err != nil {
		return nil, err
	}

	return &ContainerdService{
		client,
	}, nil
}

func (service *ContainerdService) CgroupFreeze(ctx context.Context, id string) (containerd.Task, error) {

	container, err := service.client.LoadContainer(ctx, id)
	if err != nil {
		return nil, err
	}

	task, err := container.Task(ctx, nil)
	if err != nil {
		if !errdefs.IsNotFound(err) {
			return nil, err
		}
	}
	// pause if running
	if task != nil {
		if err := task.Pause(ctx); err != nil {
			return nil, err
		}
		return task, nil
	}

	return nil, nil
}

func (service *ContainerdService) DumpRootfs(ctx context.Context, containerID, imageRef, ns string) (string, error) {
	ctx = namespaces.WithNamespace(ctx, ns)

	if err := container.ContainerdRootfsCheckpoint(ctx, service.client, containerID, imageRef); err != nil {
		return "", err
	}

	return imageRef, nil
}

func ContainerdRootfsDump(ctx context.Context, args *task.ContainerdRootfsDumpArgs) (*task.ContainerdRootfsDumpResp, error) {
	containerdService, err := New(ctx, args.Address)
	if err != nil {
		return &task.ContainerdRootfsDumpResp{}, err
	}

	ref, err := containerdService.DumpRootfs(ctx, args.ContainerID, args.ImageRef, args.Namespace)
	if err != nil {
		return &task.ContainerdRootfsDumpResp{}, err
	}

	return &task.ContainerdRootfsDumpResp{ImageRef: ref}, nil
}

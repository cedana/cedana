package containerd

import (
	"fmt"

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

	return &ContainerdService{client}, nil
}

func (service *ContainerdService) DumpRootfs(ctx context.Context, containerID, imageRef string) (string, error) {
	// TODO add namespace opt
	ctx = namespaces.WithNamespace(ctx, "default")

	opts := []containerd.CheckpointOpts{
		containerd.WithCheckpointRuntime,
		containerd.WithCheckpointImage,
		containerd.WithCheckpointRW,
	}

	container, err := service.client.LoadContainer(ctx, containerID)
	if err != nil {
		return "", err
	}

	task, err := container.Task(ctx, nil)
	if err != nil {
		if !errdefs.IsNotFound(err) {
			return "", err
		}
	}
	// pause if running
	if task != nil {
		if err := task.Pause(ctx); err != nil {
			return "", err
		}
		defer func() {
			if err := task.Resume(ctx); err != nil {
				fmt.Println(fmt.Errorf("error resuming task: %w", err))
			}
		}()
	}

	if _, err := container.Checkpoint(ctx, imageRef, opts...); err != nil {
		return "", err
	}

	return imageRef, nil
}

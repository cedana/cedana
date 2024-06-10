package containerd

import (
	"fmt"

	"github.com/cedana/cedana/container"
	"github.com/containerd/console"
	"github.com/containerd/containerd"
	"github.com/containerd/containerd/cio"
	"github.com/containerd/containerd/cmd/ctr/commands/tasks"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/errdefs"
	"github.com/containerd/log"
	"github.com/rs/zerolog"
	"golang.org/x/net/context"
)

type ContainerdService struct {
	client *containerd.Client
	logger *zerolog.Logger
}

//func NewClient(context *cli.Context, opts ...containerd.Opt) (*containerd.Client, gocontext.Context, gocontext.CancelFunc, error) {
//timeoutOpt := containerd.WithTimeout(context.Duration("connect-timeout"))
//opts = append(opts, timeoutOpt)
//kclient, err := containerd.New(context.String("address"), opts...)

func New(ctx context.Context, address string, logger *zerolog.Logger) (*ContainerdService, error) {

	client, err := containerd.New(address)

	if err != nil {
		return nil, err
	}

	return &ContainerdService{
		client,
		logger,
	}, nil
}

func (service *ContainerdService) DumpRootfs(ctx context.Context, containerID, imageRef, ns string) (string, error) {
	ctx = namespaces.WithNamespace(ctx, ns)

	if err := container.ContainerdCheckpoint(ctx, service.client, containerID, imageRef); err != nil {
		return "", err
	}

	return imageRef, nil
}

func (service *ContainerdService) RestoreRootfs(ctx context.Context, containerID, imageRef, ns string) error {
	ctx = namespaces.WithNamespace(ctx, ns)

	checkpoint, err := service.client.GetImage(ctx, imageRef)
	if err != nil {
		if !errdefs.IsNotFound(err) {
			return err
		}
		ck, err := service.client.Fetch(ctx, imageRef)
		if err != nil {
			return err
		}
		checkpoint = containerd.NewImage(service.client, ck)
	}

	opts := []containerd.RestoreOpts{
		containerd.WithRestoreImage,
		containerd.WithRestoreRuntime,
		containerd.WithRestoreRW,
		containerd.WithRestoreSpec,
	}

	ctr, err := service.client.Restore(ctx, containerID, checkpoint, opts...)
	if err != nil {
		return err
	}
	topts := []containerd.NewTaskOpts{}
	spec, err := ctr.Spec(ctx)
	if err != nil {
		return err
	}

	useTTY := spec.Process.Terminal

	var con console.Console
	if useTTY {
		con = console.Current()
		defer con.Reset()
		if err := con.SetRaw(); err != nil {
			return err
		}
	}

	task, err := tasks.NewTask(ctx, service.client, ctr, "", con, false, "", []cio.Opt{}, topts...)
	if err != nil {
		return err
	}

	var statusC <-chan containerd.ExitStatus
	if useTTY {
		if statusC, err = task.Wait(ctx); err != nil {
			return err
		}
	}

	if err := task.Start(ctx); err != nil {
		return err
	}
	if !useTTY {
		return nil
	}

	if err := tasks.HandleConsoleResize(ctx, task, con); err != nil {
		log.G(ctx).WithError(err).Error("console resize")
	}

	status := <-statusC
	code, _, err := status.Result()
	if err != nil {
		return err
	}
	if _, err := task.Delete(ctx); err != nil {
		return err
	}

	if code != 0 {
		return fmt.Errorf("Status code: %v", code)

	}
	return nil

}

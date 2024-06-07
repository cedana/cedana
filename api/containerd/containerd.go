package containerd

import (
	"fmt"

	"github.com/containerd/console"
	"github.com/containerd/containerd"
	"github.com/containerd/containerd/cio"
	"github.com/containerd/containerd/cmd/ctr/commands/tasks"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/errdefs"
	"github.com/containerd/log"
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

func (service *ContainerdService) DumpRootfs(ctx context.Context, containerID, imageRef, ns string) (string, error) {
	// TODO add namespace opt
	ctx = namespaces.WithNamespace(ctx, ns)

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

func (service *ContainerdService) RestoreRootfs(ctx context.Context, containerID, imageRef, ns string) error {

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
		containerd.WithRestoreSpec,
		containerd.WithRestoreRuntime,
		containerd.WithRestoreRW,
	}

	ctr, err := service.client.Restore(ctx, containerID, checkpoint, opts...)
	if err != nil {
		return err
	}
	topts := []containerd.NewTaskOpts{containerd.WithTaskCheckpoint(checkpoint)}
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

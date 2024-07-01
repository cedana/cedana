package containerd

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"

	"github.com/cedana/cedana/container"
	"github.com/containerd/console"
	"github.com/containerd/containerd"
	"github.com/containerd/containerd/cio"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/errdefs"
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

	if err := container.ContainerdRootfsCheckpoint(ctx, service.client, containerID, imageRef); err != nil {
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

	// complete rootfs restore using containerd client
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

	task, err := NewTask(ctx, service.client, ctr, "", con, false, "", []cio.Opt{}, topts...)
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

	// if err := tasks.HandleConsoleResize(ctx, task, con); err != nil {
	// 	log.G(ctx).WithError(err).Error("console resize")
	// }

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

type stdinCloser struct {
	stdin  *os.File
	closer func()
}

func (s *stdinCloser) Read(p []byte) (int, error) {
	n, err := s.stdin.Read(p)
	if err == io.EOF {
		if s.closer != nil {
			s.closer()
		}
	}
	return n, err
}

// NewTask creates a new task
func NewTask(ctx context.Context, client *containerd.Client, container containerd.Container, checkpoint string, con console.Console, nullIO bool, logURI string, ioOpts []cio.Opt, opts ...containerd.NewTaskOpts) (containerd.Task, error) {
	stdinC := &stdinCloser{
		stdin: os.Stdin,
	}
	if checkpoint != "" {
		im, err := client.GetImage(ctx, checkpoint)
		if err != nil {
			return nil, err
		}
		opts = append(opts, containerd.WithTaskCheckpoint(im))
	}

	spec, err := container.Spec(ctx)
	if err != nil {
		return nil, err
	}
	if spec.Linux != nil {
		if len(spec.Linux.UIDMappings) != 0 {
			opts = append(opts, containerd.WithUIDOwner(spec.Linux.UIDMappings[0].HostID))
		}
		if len(spec.Linux.GIDMappings) != 0 {
			opts = append(opts, containerd.WithGIDOwner(spec.Linux.GIDMappings[0].HostID))
		}
	}

	var ioCreator cio.Creator
	if con != nil {
		if nullIO {
			return nil, errors.New("tty and null-io cannot be used together")
		}
		ioCreator = cio.NewCreator(append([]cio.Opt{cio.WithStreams(con, con, nil), cio.WithTerminal}, ioOpts...)...)
	} else if nullIO {
		ioCreator = cio.NullIO
	} else if logURI != "" {
		u, err := url.Parse(logURI)
		if err != nil {
			return nil, err
		}
		ioCreator = cio.LogURI(u)
	} else {
		ioCreator = cio.NewCreator(append([]cio.Opt{cio.WithStreams(stdinC, os.Stdout, os.Stderr)}, ioOpts...)...)
	}
	t, err := container.NewTask(ctx, ioCreator, opts...)
	if err != nil {
		return nil, err
	}
	stdinC.closer = func() {
		t.CloseIO(ctx, containerd.WithStdinCloser)
	}
	return t, nil
}

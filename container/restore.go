package container

import (
	gocontext "context"
	"errors"

	"github.com/cedana/cedana/utils"
	"github.com/cedana/runc/libcontainer"
	"github.com/containerd/console"
	containerd "github.com/containerd/containerd"
	"github.com/containerd/containerd/cio"
	"github.com/containerd/containerd/cmd/ctr/commands/tasks"
	"github.com/containerd/containerd/log"
	"github.com/docker/docker/errdefs"
)

func Restore(imgPath string, containerID string) error {

	err := containerdRestore(containerID, imgPath)

	if err != nil {
		return err
	}

	return nil

}

func containerdRestore(id string, ref string) error {
	ctx := gocontext.Background()
	logger := utils.GetLogger()
	logger.Info().Msgf("restoring container %s from %s", id, ref)
	containerdClient, ctx, cancel, err := newContainerdClient(ctx)
	if err != nil {
		return err
	}
	defer cancel()

	checkpoint, err := containerdClient.GetImage(ctx, ref)
	if err != nil {
		if !errdefs.IsNotFound(err) {
			return err
		}
		// TODO (ehazlett): consider other options (always/never fetch)
		ck, err := containerdClient.Fetch(ctx, ref)
		if err != nil {
			return err
		}
		checkpoint = containerd.NewImage(containerdClient, ck)
	}

	opts := []containerd.RestoreOpts{
		containerd.WithRestoreImage,
		containerd.WithRestoreSpec,
		containerd.WithRestoreRuntime,
	}
	opts = append(opts, containerd.WithRestoreRW)

	ctr, err := containerdClient.Restore(ctx, id, checkpoint, opts...)
	if err != nil {
		return err
	}
	topts := []containerd.NewTaskOpts{}
	topts = append(topts, containerd.WithTaskCheckpoint(checkpoint))
	spec, err := ctr.Spec(ctx)
	if err != nil {
		return err
	}

	useTTY := spec.Process.Terminal
	// useTTY := true

	var con console.Console
	if useTTY {
		con = console.Current()
		defer con.Reset()
		if err := con.SetRaw(); err != nil {
			return err
		}
	}

	task, err := tasks.NewTask(ctx, containerdClient, ctr, ref, con, false, "", []cio.Opt{}, topts...)
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
		return errors.New("exit code not 0")
	}

	return nil
}

func RuncRestore(root string, imgPath string, containerid string) error {

	criuOpts := libcontainer.CriuOpts{
		ImagesDirectory: imgPath,
		WorkDirectory:   "",
	}

	opts := &RuncOpts{}

	_, err := StartContainer(opts, CT_ACT_RESTORE, &criuOpts)
	if err != nil {
		return err
	}
	return nil
}

type RuncOpts struct {
	Root            string
	ContainerId     string
	Bundle          string
	SystemdCgroup   bool
	NoPivot         bool
	NoMountFallback bool
	NoNewKeyring    bool
	Rootless        string
	NoSubreaper     bool
	Keep            bool
	ConsoleSocket   string
	Detatch         bool
	PidFile         string
	PreserveFds     int
}

// func taskStart(ctx gocontext.Context, client *containerd.Client, task containerd.Task) error {
// 	r, err := client.TaskService().Start(ctx, &apiTasks.StartRequest{
// 		ContainerID: task.ID(),
// 		ExecID:      p.id,
// 	})
// 	if err != nil {
// 		if p.io != nil {
// 			p.io.Cancel()
// 			p.io.Wait()
// 			p.io.Close()
// 		}
// 		return errdefs.FromGRPC(err)
// 	}
// 	p.pid = r.Pid
// 	return nil
// }

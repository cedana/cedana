package container

import (
	gocontext "context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/cedana/cedana/utils"
	"github.com/containerd/console"
	containerd "github.com/containerd/containerd"
	"github.com/containerd/containerd/cio"
	"github.com/containerd/containerd/cmd/ctr/commands/tasks"
	"github.com/containerd/containerd/log"
	"github.com/docker/docker/errdefs"
	rspec "github.com/opencontainers/runtime-spec/specs-go"
)

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
	Pid             int
	NetPid          int
}

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
		containerd.WithRestoreRW,
	}

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

	task, err := tasks.NewTask(ctx, containerdClient, ctr, "", con, false, "", []cio.Opt{}, topts...)
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

func RuncRestore(imgPath string, containerId string, opts RuncOpts) error {
	var spec rspec.Spec

	configPath := opts.Bundle + "/config.json"

	data, err := os.ReadFile(configPath)
	if err != nil {
		fmt.Println("Error reading config.json:", err)
		return err
	}

	if err := json.Unmarshal(data, &spec); err != nil {
		fmt.Println("Error decoding config.json:", err)
		return err
	}

	// Find where to mount to
	externalMounts := []string{}
	for _, m := range spec.Mounts {
		if m.Type == "bind" {
			externalMounts = append(externalMounts, fmt.Sprintf("mnt[%s]:%s", m.Destination, m.Source))
		}
	}

	sysboxMounts := &[]rspec.Mount{
		{
			Destination: "/lib/modules/6.5.0-1017-aws",
			Source:      "/usr/lib/modules/6.5.0-1017-aws",
		},
		{
			Destination: "/usr/src/linux-aws-6.5-headers-6.5.0-1017",
			Source:      "/usr/src/linux-aws-6.5-headers-6.5.0-1017",
		},
		{
			Destination: "/usr/src/linux-headers-6.5.0-1017-aws",
			Source:      "/usr/src/linux-headers-6.5.0-1017-aws",
		},
		{
			Destination: "/var/lib/kubelet",
			Source:      "/var/lib/sysbox/kubelet/f146fdc0c42d0a8e20ca9981c5a55e6998b75c477a04f24f4c663de451d4666a",
		},
		{
			Destination: "/var/lib/rancher/rke2",
			Source:      "/var/lib/sysbox/rancher-rke2/f146fdc0c42d0a8e20ca9981c5a55e6998b75c477a04f24f4c663de451d4666a",
		},
		{
			Destination: "/var/lib/sysbox/rancher-k3s/",
			Source:      "/var/lib/sysbox/rancher-k3s/f146fdc0c42d0a8e20ca9981c5a55e6998b75c477a04f24f4c663de451d4666a",
		},
		{
			Destination: "/var/lib/rancher/k3s",
			Source:      "/var/lib/sysbox/rancher-k3s/f146fdc0c42d0a8e20ca9981c5a55e6998b75c477a04f24f4c663de451d4666a",
		},
		{
			Destination: "/var/lib/docker",
			Source:      "/var/lib/sysbox/docker/f146fdc0c42d0a8e20ca9981c5a55e6998b75c477a04f24f4c663de451d4666a",
		},
		{
			Destination: "/var/lib/containerd/io.containerd.snapshotter.v1.overlayfs",
			Source:      "/var/lib/sysbox/containerd/f146fdc0c42d0a8e20ca9981c5a55e6998b75c477a04f24f4c663de451d4666a",
		},
	}

	// TODO make this sysbox only
	for _, m := range *sysboxMounts {
		externalMounts = append(externalMounts, fmt.Sprintf("mnt[%s]:%s", m.Destination, m.Source))
	}

	criuOpts := CriuOpts{
		ImagesDirectory: imgPath,
		WorkDirectory:   "",
		External:        externalMounts,
		MntnsCompatMode: false,
		TcpClose:        true,
	}

	runcOpts := &RuncOpts{
		Root:          opts.Root,
		ContainerId:   containerId,
		Bundle:        opts.Bundle,
		ConsoleSocket: opts.ConsoleSocket,
		PidFile:       "",
		Detatch:       opts.Detatch,
		NetPid:        opts.NetPid,
	}

	_, err = StartContainer(runcOpts, CT_ACT_RESTORE, &criuOpts)
	if err != nil {
		return err
	}
	return nil
}

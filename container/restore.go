package container

import (
	gocontext "context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
	var stateSpec rspec.Spec

	splitBundle := strings.Split(opts.Bundle, "/")
	bundleID := splitBundle[len(splitBundle)-1]

	statePath := filepath.Join("/run/docker/runtime-runc/moby", bundleID, "state.json")
	configPath := opts.Bundle + "/config.json"

	if err := readOCISpecJson(configPath, &spec); err != nil {
		return err
	}

	if err := readOCISpecJson(statePath, &stateSpec); err != nil {
		return err
	}

	// Find where to mount to
	externalMounts := []string{}
	for _, m := range spec.Mounts {
		if m.Type == "bind" {
			externalMounts = append(externalMounts, fmt.Sprintf("mnt[%s]:%s", m.Destination, m.Source))
		}
	}

	sysboxMounts := []rspec.Mount{
		{
			Destination: "/var/lib/kubelet",
			Source:      filepath.Join("/var/lib/sysbox/kubelet", bundleID),
		},
		{
			Destination: "/var/lib/rancher/rke2",
			Source:      filepath.Join("/var/lib/sysbox/rancher-rke2", bundleID),
		},
		{
			Destination: "/var/lib/sysbox/rancher-k3s/",
			Source:      filepath.Join("/var/lib/sysbox/rancher-k3s", bundleID),
		},
		{
			Destination: "/var/lib/rancher/k3s",
			Source:      filepath.Join("/var/lib/sysbox/rancher-k3s", bundleID),
		},
		{
			Destination: "/var/lib/docker",
			Source:      filepath.Join("/var/lib/sysbox/docker", bundleID),
		},
		{
			Destination: "/var/lib/containerd/io.containerd.snapshotter.v1.overlayfs",
			Source:      filepath.Join("/var/lib/sysbox/containerd", bundleID),
		},
		{
			Destination: "/var/lib/buildkit",
			Source:      filepath.Join("/var/lib/sysbox/buildkit", bundleID),
		},
		{
			Destination: "/var/lib/k0s",
			Source:      filepath.Join("/var/lib/sysbox/k0s", bundleID),
		},

		{
			Destination: "/dev/kmsg",
			Source:      "/null",
		},
		{
			Destination: "/proc/uptime",
			Source:      "/proc/uptime",
		},
		{
			Destination: "/proc/sys",
			Source:      "/proc/sys",
		},
		{
			Destination: "/proc/swaps",
			Source:      "/proc/swaps",
		},
		{
			Destination: "/sys/module/nf_conntrack/parameters",
			Source:      "/sys/module/nf_conntrack/parameters",
		},
		{
			Destination: "/sys/kernel",
			Source:      "/sys/kernel",
		},
		{
			Destination: "/sys/devices/virtual",
			Source:      "/sys/devices/virtual",
		},
	}

	srcFiles, err := os.ReadDir("/usr/src/")
	if err != nil {
		return err
	}

	for _, file := range srcFiles {
		mount := rspec.Mount{
			Destination: filepath.Join("/usr/src", file.Name()),
			Source:      filepath.Join("/usr/src", file.Name()),
		}
		sysboxMounts = append(sysboxMounts, mount)
	}

	moduleFiles, err := os.ReadDir("/usr/lib/modules/")
	if err != nil {
		return err
	}

	for _, file := range moduleFiles {
		mount := rspec.Mount{
			Destination: filepath.Join("/lib/modules", file.Name()),
			Source:      filepath.Join("/usr/lib/modules", file.Name()),
		}
		sysboxMounts = append(sysboxMounts, mount)
	}

	// TODO make this sysbox only
	for _, m := range sysboxMounts {
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

func readOCISpecJson(path string, spec *rspec.Spec) error {
	configData, err := os.ReadFile(path)
	if err != nil {
		fmt.Println("Error reading config.json:", err)
		return err
	}

	if err := json.Unmarshal(configData, &spec); err != nil {
		fmt.Println("Error decoding config.json:", err)
		return err
	}

	return nil
}

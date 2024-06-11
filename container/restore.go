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
	"github.com/cedana/runc/libcontainer"
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
	StateRoot       string
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

	if err := readJSON(configPath, &spec); err != nil {
		return err
	}

	// Find where to mount to
	externalMounts := []string{}
	for _, m := range spec.Mounts {
		if m.Type == "bind" {
			externalMounts = append(externalMounts, fmt.Sprintf("mnt[%s]:%s", m.Destination, m.Source))
		}
	}

	if opts.Root != "" {
		var baseState libcontainer.BaseState

		split := strings.Split(opts.Bundle, "/")
		containerID := split[len(split)-1]

		stateJsonPath := filepath.Join(opts.Root, containerID, "state.json")

		// skip if the file does not exist, allowing for a clean restore as well
		if _, err := os.Stat(stateJsonPath); err == nil {
			// if it exists we dont accept an error
			if err := readJSON(stateJsonPath, &baseState); err != nil {
				return err
			}

			for _, m := range baseState.Config.Mounts {
				if m.Device == "bind" {
					externalMounts = append(externalMounts, fmt.Sprintf("mnt[%s]:%s", m.Destination, m.Source))
				}
			}
		}
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

	_, err := StartContainer(runcOpts, CT_ACT_RESTORE, &criuOpts)
	if err != nil {
		return err
	}

	return nil
}

func readJSON[T any](path string, spec *T) error {
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

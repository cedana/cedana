package gpu

// Runc GPU interception utilities

import (
	"fmt"
	"strings"

	"github.com/opencontainers/runtime-spec/specs-go"
)

func AddGPUInterceptionToSpec(spec *specs.Spec, libraryPath string, id string) error {
	// HACK: Remove nvidia prestart hook as we don't support working around it, yet
	if spec.Hooks != nil {
		for i, hook := range spec.Hooks.Prestart {
			if strings.Contains(hook.Path, "nvidia") {
				spec.Hooks.Prestart = append(spec.Hooks.Prestart[:i], spec.Hooks.Prestart[i+1:]...)
				break
			}
		}
	}
	// skip any default /dev/shm binding in k8s
	var mounts []specs.Mount
	for _, v := range spec.Mounts {
		if v.Source != "/dev/shm" && v.Destination != "/dev/shm" {
			mounts = append(mounts, v)
		}
	}
	spec.Mounts = mounts
	spec.Mounts = append(spec.Mounts, specs.Mount{
		Destination: "/dev/shm/cedana-gpu.container",
		Source:      "/dev/shm/cedana-gpu." + id,
		Type:        "bind",
		Options:     []string{"rbind", "rprivate", "nosuid", "nodev", "rw"},
	})

	spec.Mounts = append(spec.Mounts, specs.Mount{
		Destination: "/dev/shm/cedana-gpu.container.misc",
		Source:      "/dev/shm/cedana-gpu." + id + ".misc",
		Type:        "bind",
		Options:     []string{"rbind", "rprivate", "nosuid", "nodev", "rw"},
	})

	// Mount the GPU plugin library
	spec.Mounts = append(spec.Mounts, specs.Mount{
		Destination: libraryPath,
		Source:      libraryPath,
		Type:        "bind",
		Options:     []string{"rbind", "nosuid", "nodev", "rw"},
	})

	// Set the env vars
	if spec.Process == nil {
		return fmt.Errorf("spec does not have a process")
	}
	spec.Process.Env = append(spec.Process.Env, "LD_PRELOAD="+libraryPath)
	spec.Process.Env = append(spec.Process.Env, "NCCL_SHM_DISABLE=1") // Disable for now to avoid conflicts with NCCL's shm usage
	spec.Process.Env = append(spec.Process.Env, "CEDANA_GPU_ID=container")

	return nil
}

package utils

import (
	"os"
	"path/filepath"

	podman_proto "buf.build/gen/go/cedana/cedana/protocolbuffers/go/plugins/podman"
	podman_keys "github.com/cedana/cedana/plugins/podman/pkg/keys"
)

// Supported low-level runtimes (plugins), with their state root. podman does
// not pass --root to the runtime, so these are the runtimes' defaults. Also
// the per-container state file of each runtime, to tell them apart.
var RUNTIMES = []struct {
	Plugin    string
	Root      string
	StateFile string
}{
	{Plugin: "crun", Root: "/run/crun", StateFile: "status"},
	{Plugin: "runc", Root: "/run/runc", StateFile: "state.json"},
}

// PluginForContainer returns the plugin that handles the container's low-level
// runtime, and the runtime's state root. Empty if the runtime is unsupported.
func PluginForContainer(container *Container) (plugin string, root string) {
	// Containers run/restored by cedana use cedana-oci-runtime, which records the
	// real runtime in an annotation
	if runtime := container.Config.Annotations[podman_keys.RUNTIME_ANNOTATION]; runtime != "" {
		return runtime, RootForPlugin(runtime)
	}

	runtime := filepath.Base(container.OCIRuntime)
	for _, r := range RUNTIMES {
		if r.Plugin == runtime {
			return r.Plugin, r.Root
		}
	}

	// Some other runtime that wraps a supported one (e.g. cedana-oci-runtime
	// used directly), so find the runtime that holds the container's state
	for _, r := range RUNTIMES {
		if _, err := os.Stat(filepath.Join(r.Root, container.ID, r.StateFile)); err == nil {
			return r.Plugin, r.Root
		}
	}

	return "", ""
}

func RootForPlugin(plugin string) string {
	for _, r := range RUNTIMES {
		if r.Plugin == plugin {
			return r.Root
		}
	}
	return ""
}

func Runtime(container *podman_proto.Podman) string {
	if container.Runc != nil {
		for _, r := range RUNTIMES {
			if r.Root == container.Runc.Root {
				return r.Plugin
			}
		}
	}
	return "unsupported"
}

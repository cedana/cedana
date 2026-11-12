package utils

import (
	"context"
	"path/filepath"
	"regexp"
	"strings"

	containerd_proto "buf.build/gen/go/cedana/cedana/protocolbuffers/go/plugins/containerd"
	"github.com/cedana/cedana/plugins/containerd/internal/defaults"
	"github.com/containerd/containerd"
	"github.com/containerd/containerd/api/types/runc/options"
	"github.com/containerd/typeurl/v2"
)

const (
	RUNTIME_PATTERN = "io.containerd.(.*).v[0-9]+"

	// The runtime may instead be a path to a shim binary, e.g. for containers
	// created by cedana, which use the cedana shim as the container's runtime
	SHIM_BINARY_PATTERN = "(?:containerd|cedana)-shim-(.*)-v[0-9]+$"
)

// Some runtime binaries are thin wrappers around another runtime, and their
// containers are handled by the wrapped runtime's plugin. E.g. the (legacy)
// nvidia-container-runtime just injects NVIDIA's prestart hook into the spec
// and execs runc, so its containers are runc containers.
var WRAPPER_RUNTIME_BINARIES = map[string]string{
	"nvidia-container-runtime": "runc",
}

// E.g. io.containerd.runc.v2 -> runc, /usr/local/bin/cedana-shim-runc-v2 -> runc
func PluginForRuntime(runtime string) string {
	re := regexp.MustCompile(RUNTIME_PATTERN)
	matches := re.FindStringSubmatch(runtime)
	if len(matches) == 2 {
		return matches[1]
	}
	re = regexp.MustCompile(SHIM_BINARY_PATTERN)
	matches = re.FindStringSubmatch(filepath.Base(runtime))
	if len(matches) == 2 {
		return matches[1]
	}
	return runtime
}

// PluginForContainer returns the plugin that handles the container's runtime,
// alongside the runtime name and the runtime binary (if a custom one is set).
// Unlike PluginForRuntime, it inspects the container's runtime options, since
// runtimes like crun run under the same shim as runc (io.containerd.runc.v2)
// with just a different binary name.
func PluginForContainer(ctx context.Context, client *containerd.Client, id string) (plugin string, runtime string, binary string, err error) {
	info, err := client.ContainerService().Get(ctx, id)
	if err != nil {
		return "", "", "", err
	}

	runtime = info.Runtime.Name
	plugin = PluginForRuntime(runtime)

	if info.Runtime.Options != nil && info.Runtime.Options.GetValue() != nil {
		v, err := typeurl.UnmarshalAny(info.Runtime.Options)
		if err == nil {
			if o, ok := v.(*options.Options); ok {
				binary = o.BinaryName
			}
		}
	}

	if plugin == "runc" && binary != "" {
		plugin = pluginForBinary(binary)
	}

	return plugin, runtime, binary, nil
}

// PluginForRuntimeBinary returns the plugin that handles the given runtime
// and runtime binary combination (e.g. io.containerd.runc.v2 + crun -> crun).
func PluginForRuntimeBinary(runtime string, binary string) string {
	plugin := PluginForRuntime(runtime)
	if plugin == "runc" && binary != "" {
		return pluginForBinary(binary)
	}
	return plugin
}

func pluginForBinary(binary string) string {
	plugin := filepath.Base(binary)
	if wrapped, ok := WRAPPER_RUNTIME_BINARIES[plugin]; ok {
		return wrapped
	}
	return plugin
}

// Get the root runtime directory for the plugin in namespace (e.g. runc)
// E.g. /run/containerd/runc/default
func RootFromPlugin(plugin, namespace string) string {
	return filepath.Join(defaults.BASE_RUNTIME_DIR, plugin, namespace)
}

// Get the root runtime directory for the runtime in namespace (e.g. io.containerd.runc.v2)
// E.g. /run/containerd/runc/default
func RootFromRuntime(runtime, namespace string) string {
	plugin := PluginForRuntime(runtime)
	return RootFromPlugin(plugin, namespace)
}

// Extract the namespace from the root path
// E.g. /run/containerd/runc/default -> default
func NamespaceFromRoot(root string) string {
	parts := strings.Split(root, "/")
	return parts[len(parts)-1]
}

func Runtime(container *containerd_proto.Containerd) string {
	if container.Runc != nil {
		return "runc"
	}
	// Add other supported runtimes here as needed

	return "unsupported"
}

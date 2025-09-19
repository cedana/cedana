package utils

import (
	"path/filepath"
	"strings"

	"github.com/cedana/cedana/plugins/containerd/internal/defaults"
)

// E.g. io.containerd.runc.v2 -> runc
func PluginForRuntime(runtime string) string {
	parts := strings.Split(runtime, ".")

	if len(parts) < 4 {
		return runtime
	}

	return parts[len(parts)-2]
}

// Get the root runtime directory for the plugin in namespace (e.g. runc)
// E.g. /run/containerd/runc/default
func RootFromPlugin(plugin, namespace string) string {
	return filepath.Join(defaults.BASE_RUNTIME_DIR, plugin, namespace)
}

// Extract the namespace from the root path
// E.g. /run/containerd/runc/default -> default
func NamespaceFromRoot(root string) string {
	parts := strings.Split(root, "/")
	return parts[len(parts)-1]
}

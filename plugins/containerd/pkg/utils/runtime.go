package utils

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/cedana/cedana/plugins/containerd/internal/defaults"
)

const RUNTIME_PATTERN = "io.containerd.(.*).v[0-9]+"

// E.g. io.containerd.runc.v2 -> runc
func PluginForRuntime(runtime string) string {
	re := regexp.MustCompile(RUNTIME_PATTERN)
	matches := re.FindStringSubmatch(runtime)
	if len(matches) == 2 {
		return matches[1]
	}
	return runtime
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

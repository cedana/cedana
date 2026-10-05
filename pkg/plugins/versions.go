package plugins

// Helpers for reporting the versions of installed plugins.

import "strings"

const VERSION_ATTRIBUTE_PREFIX = "cedana.plugin."
const VERSION_ATTRIBUTE_SUFFIX = ".version"

// InstalledVersions returns a map of plugin name to installed version for
// every plugin the manager reports as installed. Plugins whose version cannot
// be determined are reported as "unknown", so that their presence is still
// visible.
func InstalledVersions(manager Manager) map[string]string {
	versions := make(map[string]string)
	if manager == nil {
		return versions
	}

	list, err := manager.List(false)
	if err != nil {
		return versions
	}

	for _, p := range list {
		if !p.IsInstalled() {
			continue
		}
		version := p.Version
		if version == "" {
			version = "unknown"
		}
		versions[p.Name] = version
	}

	return versions
}

// VersionAttributeKey returns the attribute key under which a plugin's version
// is reported, e.g. "cedana.plugin.gpu.version" or "cedana.plugin.criu.cuda.version".
func VersionAttributeKey(name string) string {
	return VERSION_ATTRIBUTE_PREFIX + strings.ReplaceAll(name, "/", ".") + VERSION_ATTRIBUTE_SUFFIX
}

// InstalledVersionAttributes returns InstalledVersions keyed by attribute key.
func InstalledVersionAttributes(manager Manager) map[string]string {
	attrs := make(map[string]string)
	for name, version := range InstalledVersions(manager) {
		attrs[VersionAttributeKey(name)] = version
	}
	return attrs
}

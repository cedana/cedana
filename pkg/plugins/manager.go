package plugins

// Defines the plugin manager interface

type Manager interface {
	// List all plugins
	List(...Status) ([]Plugin, error)

	// Install a list of plugins
	Install(names []string) (chan int, chan string, chan error)

	// Remove a list of plugins
	Remove(names []string) (chan int, chan string, chan error)

	// Get a plugin by name
	Get(name string) *Plugin

	// Check if a plugin is installed
	IsInstalled(name string) bool
}

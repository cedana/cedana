package plugins

// Defines the plugin manager interface

type Manager interface {
	// List all plugins
	List(...Status) ([]Plugin, error)

	// Install a list of plugins
	Install(names []string) (installed chan int, msgs chan string, errs chan error)

	// Remove a list of plugins
	Remove(names []string) (removed chan int, msgs chan string, errs chan error)

	// Get a plugin by name
	Get(name string) *Plugin

	// Check if a plugin is installed
	IsInstalled(name string) bool
}

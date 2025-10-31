package plugins

import "fmt"

// Defines the plugin manager interface

type Manager interface {
	// List all plugins
	List(latest bool, filter ...string) ([]Plugin, error)

	// Install a list of plugins
	Install(names []string) (installed chan int, msgs chan string, errs chan error)

	// Remove a list of plugins
	Remove(names []string) (removed chan int, msgs chan string, errs chan error)

	// Get a plugin by name
	Get(name string) *Plugin

	//  Get by formatted name
	Getf(format string, a ...any) *Plugin

	// Check if a plugin is installed
	IsInstalled(name string) bool
}

type ManagerUnimplemented struct{}

func (m *ManagerUnimplemented) List(_ ...Status) ([]Plugin, error) {
	return nil, fmt.Errorf("List not implemented")
}

func (m *ManagerUnimplemented) Install(_ []string) (chan int, chan string, chan error) {
	errs := make(chan error)
	errs <- fmt.Errorf("Install not implemented")
	close(errs)
	return nil, nil, errs
}

func (m *ManagerUnimplemented) Remove(_ []string) (chan int, chan string, chan error) {
	errs := make(chan error)
	errs <- fmt.Errorf("Remove not implemented")
	close(errs)
	return nil, nil, errs
}

func (m *ManagerUnimplemented) Get(_ string) *Plugin {
	return nil
}

func (m *ManagerUnimplemented) Getf(format string, a ...any) *Plugin {
	return nil
}

func (m *ManagerUnimplemented) IsInstalled(_ string) bool {
	return false
}

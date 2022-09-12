package utils

import "os/exec"

// implement go-criu's Notify interface
// type Notify interface {
//	PreDump() error
//	PostDump() error
//	PreRestore() error
//	PostRestore(pid int32) error
//	NetworkLock() error
//	NetworkUnlock() error
//	SetupNamespaces(pid int32) error
//	PostSetupNamespaces() error
//	PostResume() error
//}

type Notify struct {
	config           *Config
	PreDumpAvail     bool
	PostDumpAvail    bool
	PreRestoreAvail  bool
}

// PreDump NoNotify
func (n Notify) PreDump() error {
	if n.PreDumpAvail {
		exec.Command("/bin/sh", n.config.ActionScripts.PreDump).Run()
	}
	return nil
}

// PostDump NoNotify
func (n Notify) PostDump() error {
	if n.PostDumpAvail {
		exec.Command("/bin/sh", n.config.ActionScripts.PostDump).Run()
	}
	return nil
}

// PreRestore NoNotify
func (n Notify) PreRestore() error {
	if n.PreRestoreAvail {
		exec.Command("/bin/sh", n.config.ActionScripts.PreRestore).Run()
	}
	return nil
}

// PostRestore NoNotify
func (n Notify) PostRestore(pid int32) error {
	return nil
}

// NetworkLock NoNotify
func (n Notify) NetworkLock() error {
	return nil
}

// NetworkUnlock NoNotify
func (n Notify) NetworkUnlock() error {
	return nil
}

// SetupNamespaces NoNotify
func (n Notify) SetupNamespaces(pid int32) error {
	return nil
}

// PostSetupNamespaces NoNotify
func (n Notify) PostSetupNamespaces() error {
	return nil
}

// PostResume NoNotify
func (n Notify) PostResume() error {
	return nil
}

package criu

// Notify interface
type Notify interface {
	PreDump() error
	PostDump() error
	PreRestore() error
	PostRestore(pid int32) error
	NetworkLock() error
	NetworkUnlock() error
	SetupNamespaces(pid int32) error
	PostSetupNamespaces(pid int32) error
	PreResume(pid int32) error
	PostResume(pid int32) error
	OrphanPtsMaster(fd int32) error
}

// NoNotify struct
type NoNotify struct{}

// PreDump NoNotify
func (c NoNotify) PreDump() error {
	return nil
}

// PostDump NoNotify
func (c NoNotify) PostDump() error {
	return nil
}

// PreRestore NoNotify
func (c NoNotify) PreRestore() error {
	return nil
}

// PostRestore NoNotify
func (c NoNotify) PostRestore(pid int32) error {
	return nil
}

// NetworkLock NoNotify
func (c NoNotify) NetworkLock() error {
	return nil
}

// NetworkUnlock NoNotify
func (c NoNotify) NetworkUnlock() error {
	return nil
}

// SetupNamespaces NoNotify
func (c NoNotify) SetupNamespaces(pid int32) error {
	return nil
}

// PostSetupNamespaces NoNotify
func (c NoNotify) PostSetupNamespaces(pid int32) error {
	return nil
}

// PreResume NoNotify
func (c NoNotify) PreResume(pid int32) error {
	return nil
}

// PostResume NoNotify
func (c NoNotify) PostResume(pid int32) error {
	return nil
}

func (c NoNotify) OrphanPtsMaster(fd int32) error {
	return nil
}

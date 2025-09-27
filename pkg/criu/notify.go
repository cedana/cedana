package criu

import (
	"context"

	"buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
)

// Notify interface
type Notify interface {
	Initialize(ctx context.Context, criuPid int32) error
	InitializeDump(ctx context.Context, opts *criu.CriuOpts) error
	InitializeRestore(ctx context.Context, opts *criu.CriuOpts) error
	FinalizeDump(ctx context.Context, opts *criu.CriuOpts) error
	FinalizeRestore(ctx context.Context, opts *criu.CriuOpts) error
	PreDump(ctx context.Context, opts *criu.CriuOpts) error
	PostDump(ctx context.Context, opts *criu.CriuOpts) error
	PreRestore(ctx context.Context, opts *criu.CriuOpts) error
	PostRestore(ctx context.Context, pid int32) error
	NetworkLock(ctx context.Context) error
	NetworkUnlock(ctx context.Context) error
	SetupNamespaces(ctx context.Context, pid int32) error
	PostSetupNamespaces(ctx context.Context) error
	PreResume(ctx context.Context) error
	PostResume(ctx context.Context) error
	OrphanPtsMaster(ctx context.Context, fd int32) error
}

// NoNotify struct
type NoNotify struct{}

// Initialize NoNotify
func (c NoNotify) Initialize(ctx context.Context, criuPid int) error {
	return nil
}

// InitializeDump NoNotify
func (c NoNotify) InitializeDump(ctx context.Context, opts *criu.CriuOpts) error {
	return nil
}

// InitializeRestore NoNotify
func (c NoNotify) InitializeRestore(ctx context.Context, opts *criu.CriuOpts) error {
	return nil
}

// FinalizeDump NoNotify
func (c NoNotify) FinalizeDump(ctx context.Context, opts *criu.CriuOpts) error {
	return nil
}

// FinalizeRestore NoNotify
func (c NoNotify) FinalizeRestore(ctx context.Context, opts *criu.CriuOpts) error {
	return nil
}

// PreDump NoNotify
func (c NoNotify) PreDump(ctx context.Context, dir string) error {
	return nil
}

// PostDump NoNotify
func (c NoNotify) PostDump(ctx context.Context, dir string) error {
	return nil
}

// PreRestore NoNotify
func (c NoNotify) PreRestore(ctx context.Context, dir string) error {
	return nil
}

// PostRestore NoNotify
func (c NoNotify) PostRestore(ctx context.Context, pid int32) error {
	return nil
}

// NetworkLock NoNotify
func (c NoNotify) NetworkLock(ctx context.Context) error {
	return nil
}

// NetworkUnlock NoNotify
func (c NoNotify) NetworkUnlock(ctx context.Context) error {
	return nil
}

// SetupNamespaces NoNotify
func (c NoNotify) SetupNamespaces(ctx context.Context, pid int32) error {
	return nil
}

// PostSetupNamespaces NoNotify
func (c NoNotify) PostSetupNamespaces(ctx context.Context) error {
	return nil
}

// PreResume NoNotify
func (c NoNotify) PreResume(ctx context.Context) error {
	return nil
}

// PostResume NoNotify
func (c NoNotify) PostResume(ctx context.Context) error {
	return nil
}

func (c NoNotify) OrphanPtsMaster(ctx context.Context, fd int32) error {
	return nil
}

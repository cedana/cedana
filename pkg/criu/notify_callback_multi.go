package criu

// Implements the Notify interface to support registering multiple callbacks
// Callbacks are called in the reverse order they were registered
// For registration, new callbacks must be appended to the end of the exiting list

import (
	"context"

	"buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
)

type NotifyCallbackMulti struct {
	InitializeFunc          []InitializeFunc
	PreDumpFunc             []NotifyFuncOpts
	PostDumpFunc            []NotifyFuncOpts
	PreRestoreFunc          []NotifyFuncOpts
	PostRestoreFunc         []NotifyFuncPid
	NetworkLockFunc         []NotifyFunc
	NetworkUnlockFunc       []NotifyFunc
	SetupNamespacesFunc     []NotifyFuncPid
	PostSetupNamespacesFunc []NotifyFuncPid
	PreResumeFunc           []NotifyFuncPid
	PostResumeFunc          []NotifyFuncPid
	OrphanPtsMasterFunc     []NotifyFuncFd
}

func (n *NotifyCallbackMulti) ImportCallback(nfy *NotifyCallback) {
	if nfy == nil {
		return
	}
	if nfy.InitializeFunc != nil {
		n.InitializeFunc = append(n.InitializeFunc, nfy.InitializeFunc)
	}
	if nfy.PreDumpFunc != nil {
		n.PreDumpFunc = append(n.PreDumpFunc, nfy.PreDumpFunc)
	}
	if nfy.PostDumpFunc != nil {
		n.PostDumpFunc = append(n.PostDumpFunc, nfy.PostDumpFunc)
	}
	if nfy.PreRestoreFunc != nil {
		n.PreRestoreFunc = append(n.PreRestoreFunc, nfy.PreRestoreFunc)
	}
	if nfy.PostRestoreFunc != nil {
		n.PostRestoreFunc = append(n.PostRestoreFunc, nfy.PostRestoreFunc)
	}
	if nfy.NetworkLockFunc != nil {
		n.NetworkLockFunc = append(n.NetworkLockFunc, nfy.NetworkLockFunc)
	}
	if nfy.NetworkUnlockFunc != nil {
		n.NetworkUnlockFunc = append(n.NetworkUnlockFunc, nfy.NetworkUnlockFunc)
	}
	if nfy.SetupNamespacesFunc != nil {
		n.SetupNamespacesFunc = append(n.SetupNamespacesFunc, nfy.SetupNamespacesFunc)
	}
	if nfy.PostSetupNamespacesFunc != nil {
		n.PostSetupNamespacesFunc = append(n.PostSetupNamespacesFunc, nfy.PostSetupNamespacesFunc)
	}
	if nfy.PreResumeFunc != nil {
		n.PreResumeFunc = append(n.PreResumeFunc, nfy.PreResumeFunc)
	}
	if nfy.PostResumeFunc != nil {
		n.PostResumeFunc = append(n.PostResumeFunc, nfy.PostResumeFunc)
	}
	if nfy.OrphanPtsMasterFunc != nil {
		n.OrphanPtsMasterFunc = append(n.OrphanPtsMasterFunc, nfy.OrphanPtsMasterFunc)
	}
}

func (n *NotifyCallbackMulti) Initialize(ctx context.Context, criuPid int) error {
	for i := len(n.InitializeFunc) - 1; i >= 0; i-- {
		err := n.InitializeFunc[i](ctx, criuPid)
		if err != nil {
			return err
		}
	}
	return nil
}

func (n *NotifyCallbackMulti) PreDump(ctx context.Context, opts *criu.CriuOpts) error {
	for i := len(n.PreDumpFunc) - 1; i >= 0; i-- {
		err := n.PreDumpFunc[i](ctx, opts)
		if err != nil {
			return err
		}
	}
	return nil
}

func (n *NotifyCallbackMulti) PostDump(ctx context.Context, opts *criu.CriuOpts) error {
	for i := len(n.PostDumpFunc) - 1; i >= 0; i-- {
		err := n.PostDumpFunc[i](ctx, opts)
		if err != nil {
			return err
		}
	}
	return nil
}

func (n *NotifyCallbackMulti) PreRestore(ctx context.Context, opts *criu.CriuOpts) error {
	for i := len(n.PreRestoreFunc) - 1; i >= 0; i-- {
		err := n.PreRestoreFunc[i](ctx, opts)
		if err != nil {
			return err
		}
	}
	return nil
}

func (n *NotifyCallbackMulti) PreResume(ctx context.Context, pid int32) error {
	for i := len(n.PreResumeFunc) - 1; i >= 0; i-- {
		err := n.PreResumeFunc[i](ctx, pid)
		if err != nil {
			return err
		}
	}
	return nil
}

func (n *NotifyCallbackMulti) PostResume(ctx context.Context, pid int32) error {
	for i := len(n.PostResumeFunc) - 1; i >= 0; i-- {
		err := n.PostResumeFunc[i](ctx, pid)
		if err != nil {
			return err
		}
	}
	return nil
}

func (n *NotifyCallbackMulti) PostRestore(ctx context.Context, pid int32) error {
	for i := len(n.PostRestoreFunc) - 1; i >= 0; i-- {
		err := n.PostRestoreFunc[i](ctx, pid)
		if err != nil {
			return err
		}
	}
	return nil
}

func (n *NotifyCallbackMulti) NetworkLock(ctx context.Context) error {
	for i := len(n.NetworkLockFunc) - 1; i >= 0; i-- {
		err := n.NetworkLockFunc[i](ctx)
		if err != nil {
			return err
		}
	}
	return nil
}

func (n *NotifyCallbackMulti) NetworkUnlock(ctx context.Context) error {
	for i := len(n.NetworkUnlockFunc) - 1; i >= 0; i-- {
		err := n.NetworkUnlockFunc[i](ctx)
		if err != nil {
			return err
		}
	}
	return nil
}

func (n *NotifyCallbackMulti) SetupNamespaces(ctx context.Context, pid int32) error {
	for i := len(n.SetupNamespacesFunc) - 1; i >= 0; i-- {
		err := n.SetupNamespacesFunc[i](ctx, pid)
		if err != nil {
			return err
		}
	}
	return nil
}

func (n *NotifyCallbackMulti) PostSetupNamespaces(ctx context.Context, pid int32) error {
	for i := len(n.PostSetupNamespacesFunc) - 1; i >= 0; i-- {
		err := n.PostSetupNamespacesFunc[i](ctx, pid)
		if err != nil {
			return err
		}
	}
	return nil
}

func (n *NotifyCallbackMulti) OrphanPtsMaster(ctx context.Context, fd int32) error {
	for i := len(n.OrphanPtsMasterFunc) - 1; i >= 0; i-- {
		err := n.OrphanPtsMasterFunc[i](ctx, fd)
		if err != nil {
			return err
		}
	}
	return nil
}

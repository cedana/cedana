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

func (n *NotifyCallbackMulti) Include(nfy NotifyCallback) {
	n.InitializeFunc = append(n.InitializeFunc, nfy.InitializeFunc)
	n.PreDumpFunc = append(n.PreDumpFunc, nfy.PreDumpFunc)
	n.PostDumpFunc = append(n.PostDumpFunc, nfy.PostDumpFunc)
	n.PreRestoreFunc = append(n.PreRestoreFunc, nfy.PreRestoreFunc)
	n.PostRestoreFunc = append(n.PostRestoreFunc, nfy.PostRestoreFunc)
	n.NetworkLockFunc = append(n.NetworkLockFunc, nfy.NetworkLockFunc)
	n.NetworkUnlockFunc = append(n.NetworkUnlockFunc, nfy.NetworkUnlockFunc)
	n.SetupNamespacesFunc = append(n.SetupNamespacesFunc, nfy.SetupNamespacesFunc)
	n.PostSetupNamespacesFunc = append(n.PostSetupNamespacesFunc, nfy.PostSetupNamespacesFunc)
	n.PreResumeFunc = append(n.PreResumeFunc, nfy.PreResumeFunc)
	n.PostResumeFunc = append(n.PostResumeFunc, nfy.PostResumeFunc)
	n.OrphanPtsMasterFunc = append(n.OrphanPtsMasterFunc, nfy.OrphanPtsMasterFunc)
}

func (n *NotifyCallbackMulti) IncludeMulti(nfy NotifyCallbackMulti) {
	n.InitializeFunc = append(n.InitializeFunc, nfy.InitializeFunc...)
	n.PreDumpFunc = append(n.PreDumpFunc, nfy.PreDumpFunc...)
	n.PostDumpFunc = append(n.PostDumpFunc, nfy.PostDumpFunc...)
	n.PreRestoreFunc = append(n.PreRestoreFunc, nfy.PreRestoreFunc...)
	n.PostRestoreFunc = append(n.PostRestoreFunc, nfy.PostRestoreFunc...)
	n.NetworkLockFunc = append(n.NetworkLockFunc, nfy.NetworkLockFunc...)
	n.NetworkUnlockFunc = append(n.NetworkUnlockFunc, nfy.NetworkUnlockFunc...)
	n.SetupNamespacesFunc = append(n.SetupNamespacesFunc, nfy.SetupNamespacesFunc...)
	n.PostSetupNamespacesFunc = append(n.PostSetupNamespacesFunc, nfy.PostSetupNamespacesFunc...)
	n.PreResumeFunc = append(n.PreResumeFunc, nfy.PreResumeFunc...)
	n.PostResumeFunc = append(n.PostResumeFunc, nfy.PostResumeFunc...)
	n.OrphanPtsMasterFunc = append(n.OrphanPtsMasterFunc, nfy.OrphanPtsMasterFunc...)
}

func (n NotifyCallbackMulti) Initialize(ctx context.Context, criuPid int32) error {
	for i := len(n.InitializeFunc) - 1; i >= 0; i-- {
		if n.InitializeFunc[i] == nil {
			continue
		}
		err := n.InitializeFunc[i](ctx, criuPid)
		if err != nil {
			return err
		}
	}
	return nil
}

func (n NotifyCallbackMulti) PreDump(ctx context.Context, opts *criu.CriuOpts) error {
	for i := len(n.PreDumpFunc) - 1; i >= 0; i-- {
		if n.PreDumpFunc[i] == nil {
			continue
		}
		err := n.PreDumpFunc[i](ctx, opts)
		if err != nil {
			return err
		}
	}
	return nil
}

func (n NotifyCallbackMulti) PostDump(ctx context.Context, opts *criu.CriuOpts) error {
	for i := len(n.PostDumpFunc) - 1; i >= 0; i-- {
		if n.PostDumpFunc[i] == nil {
			continue
		}
		err := n.PostDumpFunc[i](ctx, opts)
		if err != nil {
			return err
		}
	}
	return nil
}

func (n NotifyCallbackMulti) PreRestore(ctx context.Context, opts *criu.CriuOpts) error {
	for i := len(n.PreRestoreFunc) - 1; i >= 0; i-- {
		if n.PreRestoreFunc[i] == nil {
			continue
		}
		err := n.PreRestoreFunc[i](ctx, opts)
		if err != nil {
			return err
		}
	}
	return nil
}

func (n NotifyCallbackMulti) PreResume(ctx context.Context, pid int32) error {
	for i := len(n.PreResumeFunc) - 1; i >= 0; i-- {
		if n.PreResumeFunc[i] == nil {
			continue
		}
		err := n.PreResumeFunc[i](ctx, pid)
		if err != nil {
			return err
		}
	}
	return nil
}

func (n NotifyCallbackMulti) PostResume(ctx context.Context, pid int32) error {
	for i := len(n.PostResumeFunc) - 1; i >= 0; i-- {
		if n.PostResumeFunc[i] == nil {
			continue
		}
		err := n.PostResumeFunc[i](ctx, pid)
		if err != nil {
			return err
		}
	}
	return nil
}

func (n NotifyCallbackMulti) PostRestore(ctx context.Context, pid int32) error {
	for i := len(n.PostRestoreFunc) - 1; i >= 0; i-- {
		if n.PostRestoreFunc[i] == nil {
			continue
		}
		err := n.PostRestoreFunc[i](ctx, pid)
		if err != nil {
			return err
		}
	}
	return nil
}

func (n NotifyCallbackMulti) NetworkLock(ctx context.Context) error {
	for i := len(n.NetworkLockFunc) - 1; i >= 0; i-- {
		if n.NetworkLockFunc[i] == nil {
			continue
		}
		err := n.NetworkLockFunc[i](ctx)
		if err != nil {
			return err
		}
	}
	return nil
}

func (n NotifyCallbackMulti) NetworkUnlock(ctx context.Context) error {
	for i := len(n.NetworkUnlockFunc) - 1; i >= 0; i-- {
		if n.NetworkUnlockFunc[i] == nil {
			continue
		}
		err := n.NetworkUnlockFunc[i](ctx)
		if err != nil {
			return err
		}
	}
	return nil
}

func (n NotifyCallbackMulti) SetupNamespaces(ctx context.Context, pid int32) error {
	for i := len(n.SetupNamespacesFunc) - 1; i >= 0; i-- {
		if n.SetupNamespacesFunc[i] == nil {
			continue
		}
		err := n.SetupNamespacesFunc[i](ctx, pid)
		if err != nil {
			return err
		}
	}
	return nil
}

func (n NotifyCallbackMulti) PostSetupNamespaces(ctx context.Context, pid int32) error {
	for i := len(n.PostSetupNamespacesFunc) - 1; i >= 0; i-- {
		if n.PostSetupNamespacesFunc[i] == nil {
			continue
		}
		err := n.PostSetupNamespacesFunc[i](ctx, pid)
		if err != nil {
			return err
		}
	}
	return nil
}

func (n NotifyCallbackMulti) OrphanPtsMaster(ctx context.Context, fd int32) error {
	for i := len(n.OrphanPtsMasterFunc) - 1; i >= 0; i-- {
		if n.OrphanPtsMasterFunc[i] == nil {
			continue
		}
		err := n.OrphanPtsMasterFunc[i](ctx, fd)
		if err != nil {
			return err
		}
	}
	return nil
}

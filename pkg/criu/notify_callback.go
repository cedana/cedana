package criu

// Implements the Notify interface to support callbacks

import (
	"context"

	"buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
)

type NotifyCallback struct {
	InitializeFunc          InitializeFunc
	PreDumpFunc             NotifyFuncOpts
	PostDumpFunc            NotifyFuncOpts
	PreRestoreFunc          NotifyFuncOpts
	PostRestoreFunc         NotifyFuncPid
	NetworkLockFunc         NotifyFunc
	NetworkUnlockFunc       NotifyFunc
	SetupNamespacesFunc     NotifyFuncPid
	PostSetupNamespacesFunc NotifyFuncPid
	PreResumeFunc           NotifyFuncPid
	PostResumeFunc          NotifyFuncPid
	OrphanPtsMasterFunc     NotifyFuncFd
}

type (
	NotifyFunc     func(ctx context.Context) error
	NotifyFuncOpts func(ctx context.Context, opts *criu.CriuOpts) error
	NotifyFuncPid  func(ctx context.Context, pid int32) error
	NotifyFuncFd   func(ctx context.Context, fd int32) error
	InitializeFunc func(ctx context.Context, criuPid int32) error
)

func (n NotifyCallback) Initialize(ctx context.Context, criuPid int32) error {
	if n.InitializeFunc != nil {
		err := n.InitializeFunc(ctx, criuPid)
		if err != nil {
			return err
		}
	}
	return nil
}

func (n NotifyCallback) PreDump(ctx context.Context, opts *criu.CriuOpts) error {
	if n.PreDumpFunc != nil {
		err := n.PreDumpFunc(ctx, opts)
		if err != nil {
			return err
		}
	}
	return nil
}

func (n NotifyCallback) PostDump(ctx context.Context, opts *criu.CriuOpts) error {
	if n.PostDumpFunc != nil {
		err := n.PostDumpFunc(ctx, opts)
		if err != nil {
			return err
		}
	}
	return nil
}

func (n NotifyCallback) PreRestore(ctx context.Context, opts *criu.CriuOpts) error {
	if n.PreRestoreFunc != nil {
		err := n.PreRestoreFunc(ctx, opts)
		if err != nil {
			return err
		}
	}
	return nil
}

func (n NotifyCallback) PreResume(ctx context.Context, pid int32) error {
	if n.PreResumeFunc != nil {
		err := n.PreResumeFunc(ctx, pid)
		if err != nil {
			return err
		}
	}
	return nil
}

func (n NotifyCallback) PostRestore(ctx context.Context, pid int32) error {
	if n.PostRestoreFunc != nil {
		err := n.PostRestoreFunc(ctx, pid)
		if err != nil {
			return err
		}
	}
	return nil
}

func (n NotifyCallback) NetworkLock(ctx context.Context) error {
	if n.NetworkLockFunc != nil {
		err := n.NetworkLockFunc(ctx)
		if err != nil {
			return err
		}
	}
	return nil
}

func (n NotifyCallback) NetworkUnlock(ctx context.Context) error {
	if n.NetworkUnlockFunc != nil {
		err := n.NetworkUnlockFunc(ctx)
		if err != nil {
			return err
		}
	}
	return nil
}

func (n NotifyCallback) SetupNamespaces(ctx context.Context, pid int32) error {
	if n.SetupNamespacesFunc != nil {
		err := n.SetupNamespacesFunc(ctx, pid)
		if err != nil {
			return err
		}
	}
	return nil
}

func (n NotifyCallback) PostSetupNamespaces(ctx context.Context, pid int32) error {
	if n.PostSetupNamespacesFunc != nil {
		err := n.PostSetupNamespacesFunc(ctx, pid)
		if err != nil {
			return err
		}
	}
	return nil
}

func (n NotifyCallback) PostResume(ctx context.Context, pid int32) error {
	if n.PostResumeFunc != nil {
		err := n.PostResumeFunc(ctx, pid)
		if err != nil {
			return err
		}
	}
	return nil
}

func (n NotifyCallback) OrphanPtsMaster(ctx context.Context, fd int32) error {
	if n.OrphanPtsMasterFunc != nil {
		err := n.OrphanPtsMasterFunc(ctx, fd)
		if err != nil {
			return err
		}
	}
	return nil
}

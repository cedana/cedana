package criu

// Implements the Notify interface to support callbacks, and profiling.

import (
	"context"
	"fmt"

	"buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/pkg/profiling"
)

type NotifyCallback struct {
	InitializeFunc          InitializeFunc
	InitializeDumpFunc      NotifyFuncOpts
	InitializeRestoreFunc   NotifyFuncOpts
	FinalizeDumpFunc        NotifyFuncOpts
	FinalizeRestoreFunc     NotifyFuncOpts
	PreDumpFunc             NotifyFuncOpts
	PostDumpFunc            NotifyFuncOpts
	PreRestoreFunc          NotifyFuncOpts
	PostRestoreFunc         NotifyFuncPid
	NetworkLockFunc         NotifyFunc
	NetworkUnlockFunc       NotifyFunc
	SetupNamespacesFunc     NotifyFuncPid
	PostSetupNamespacesFunc NotifyFunc
	PreResumeFunc           NotifyFunc
	PostResumeFunc          NotifyFunc
	OrphanPtsMasterFunc     NotifyFuncFd

	Name string // to give some context to this callback
}

type (
	NotifyFuncOptsNoError func(ctx context.Context, opts *criu.CriuOpts)
	NotifyFunc            func(ctx context.Context) error
	NotifyFuncOpts        func(ctx context.Context, opts *criu.CriuOpts) error
	NotifyFuncPid         func(ctx context.Context, pid int32) error
	NotifyFuncFd          func(ctx context.Context, fd int32) error
	InitializeFunc        func(ctx context.Context, criuPid int32) error
)

func (n NotifyCallback) Initialize(ctx context.Context, criuPid int32) error {
	if n.InitializeFunc != nil {
		var end func()
		ctx, end = profiling.StartTimingCategory(ctx, n.Name)
		defer end()
		err := n.InitializeFunc(ctx, criuPid)
		if err != nil {
			return fmt.Errorf("initialize callback: %v", err)
		}
	}
	return nil
}

func (n NotifyCallback) InitializeDump(ctx context.Context, opts *criu.CriuOpts) error {
	if n.InitializeDumpFunc != nil {
		var end func()
		ctx, end = profiling.StartTimingCategory(ctx, n.Name)
		defer end()
		err := n.InitializeDumpFunc(ctx, opts)
		if err != nil {
			return fmt.Errorf("initialize dump callback: %v", err)
		}
	}
	return nil
}

func (n NotifyCallback) InitializeRestore(ctx context.Context, opts *criu.CriuOpts) error {
	if n.InitializeRestoreFunc != nil {
		var end func()
		ctx, end = profiling.StartTimingCategory(ctx, n.Name)
		defer end()
		err := n.InitializeRestoreFunc(ctx, opts)
		if err != nil {
			return fmt.Errorf("initialize restore callback: %v", err)
		}
	}
	return nil
}

func (n NotifyCallback) FinalizeDump(ctx context.Context, opts *criu.CriuOpts) error {
	if n.FinalizeDumpFunc != nil {
		var end func()
		ctx, end = profiling.StartTimingCategory(ctx, n.Name)
		defer end()
		err := n.FinalizeDumpFunc(ctx, opts)
		if err != nil {
			return fmt.Errorf("finalize dump callback: %v", err)
		}
	}
	return nil
}

func (n NotifyCallback) FinalizeRestore(ctx context.Context, opts *criu.CriuOpts) error {
	if n.FinalizeRestoreFunc != nil {
		var end func()
		ctx, end = profiling.StartTimingCategory(ctx, n.Name)
		defer end()
		err := n.FinalizeRestoreFunc(ctx, opts)
		if err != nil {
			return fmt.Errorf("finalize restore callback: %v", err)
		}
	}
	return nil
}

func (n NotifyCallback) PreDump(ctx context.Context, opts *criu.CriuOpts) error {
	if n.PreDumpFunc != nil {
		var end func()
		ctx, end = profiling.StartTimingCategory(ctx, n.Name)
		defer end()
		err := n.PreDumpFunc(ctx, opts)
		if err != nil {
			return fmt.Errorf("pre-dump callback: %v", err)
		}
	}
	return nil
}

func (n NotifyCallback) PostDump(ctx context.Context, opts *criu.CriuOpts) error {
	if n.PostDumpFunc != nil {
		var end func()
		ctx, end = profiling.StartTimingCategory(ctx, n.Name)
		defer end()
		err := n.PostDumpFunc(ctx, opts)
		if err != nil {
			return fmt.Errorf("post-dump callback: %v", err)
		}
	}
	return nil
}

func (n NotifyCallback) PreRestore(ctx context.Context, opts *criu.CriuOpts) error {
	if n.PreRestoreFunc != nil {
		var end func()
		ctx, end = profiling.StartTimingCategory(ctx, n.Name)
		defer end()
		err := n.PreRestoreFunc(ctx, opts)
		if err != nil {
			return fmt.Errorf("pre-restore callback: %v", err)
		}
	}
	return nil
}

func (n NotifyCallback) PreResume(ctx context.Context) error {
	if n.PreResumeFunc != nil {
		var end func()
		ctx, end = profiling.StartTimingCategory(ctx, n.Name)
		defer end()
		err := n.PreResumeFunc(ctx)
		if err != nil {
			return fmt.Errorf("pre-resume callback: %v", err)
		}
	}
	return nil
}

func (n NotifyCallback) PostRestore(ctx context.Context, pid int32) error {
	if n.PostRestoreFunc != nil {
		var end func()
		ctx, end = profiling.StartTimingCategory(ctx, n.Name)
		defer end()
		err := n.PostRestoreFunc(ctx, pid)
		if err != nil {
			return fmt.Errorf("post-restore callback: %v", err)
		}
	}
	return nil
}

func (n NotifyCallback) NetworkLock(ctx context.Context) error {
	if n.NetworkLockFunc != nil {
		var end func()
		ctx, end = profiling.StartTimingCategory(ctx, n.Name)
		defer end()
		err := n.NetworkLockFunc(ctx)
		if err != nil {
			return fmt.Errorf("network-lock callback: %v", err)
		}
	}
	return nil
}

func (n NotifyCallback) NetworkUnlock(ctx context.Context) error {
	if n.NetworkUnlockFunc != nil {
		var end func()
		ctx, end = profiling.StartTimingCategory(ctx, n.Name)
		defer end()
		err := n.NetworkUnlockFunc(ctx)
		if err != nil {
			return fmt.Errorf("network-unlock callback: %v", err)
		}
	}
	return nil
}

func (n NotifyCallback) SetupNamespaces(ctx context.Context, pid int32) error {
	if n.SetupNamespacesFunc != nil {
		var end func()
		ctx, end = profiling.StartTimingCategory(ctx, n.Name)
		defer end()
		err := n.SetupNamespacesFunc(ctx, pid)
		if err != nil {
			return fmt.Errorf("setup-namespaces callback: %v", err)
		}
	}
	return nil
}

func (n NotifyCallback) PostSetupNamespaces(ctx context.Context) error {
	if n.PostSetupNamespacesFunc != nil {
		var end func()
		ctx, end = profiling.StartTimingCategory(ctx, n.Name)
		defer end()
		err := n.PostSetupNamespacesFunc(ctx)
		if err != nil {
			return fmt.Errorf("post-setup-namespaces callback: %v", err)
		}
	}
	return nil
}

func (n NotifyCallback) PostResume(ctx context.Context) error {
	if n.PostResumeFunc != nil {
		var end func()
		ctx, end = profiling.StartTimingCategory(ctx, n.Name)
		defer end()
		err := n.PostResumeFunc(ctx)
		if err != nil {
			return fmt.Errorf("post-resume callback: %v", err)
		}
	}
	return nil
}

func (n NotifyCallback) OrphanPtsMaster(ctx context.Context, fd int32) error {
	if n.OrphanPtsMasterFunc != nil {
		var end func()
		ctx, end = profiling.StartTimingCategory(ctx, n.Name)
		defer end()
		err := n.OrphanPtsMasterFunc(ctx, fd)
		if err != nil {
			return fmt.Errorf("orphan-pts-master callback: %v", err)
		}
	}
	return nil
}

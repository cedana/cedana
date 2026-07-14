package criu

// Implements the Notify interface to support callbacks, and profiling.

import (
	"context"
	"fmt"

	"buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/pkg/profiling"
	"github.com/rs/zerolog/log"
)

type NotifyCallback struct {
	InitializeFunc          InitializeFunc
	InitializeDumpFunc      NotifyFuncOpts
	InitializeRestoreFunc   NotifyFuncOpts
	FinalizeDumpFunc        NotifyFuncOptsErr
	FinalizeRestoreFunc     NotifyFuncOptsErr
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
	NotifyFuncOptsErr     func(ctx context.Context, opts *criu.CriuOpts, err error) error
	NotifyFuncPid         func(ctx context.Context, pid int32) error
	NotifyFuncFd          func(ctx context.Context, fd int32) error
	InitializeFunc        func(ctx context.Context, criuPid int32) error
)

func (n NotifyCallback) Initialize(ctx context.Context, criuPid int32) error {
	if n.InitializeFunc != nil {
		log.Trace().Int32("criuPid", criuPid).Str("name", n.Name).Msg("CRIU initialize callback")
		var end func()
		ctx, end = profiling.StartTimingCategory(ctx, n.Name)
		defer end()
		err := n.InitializeFunc(ctx, criuPid)
		if err != nil {
			log.Trace().Err(err).Int32("criuPid", criuPid).Str("name", n.Name).Msg("CRIU initialize callback failed")
			return fmt.Errorf("initialize callback: %v", err)
		}
	}
	return nil
}

func (n NotifyCallback) InitializeDump(ctx context.Context, opts *criu.CriuOpts) error {
	if n.InitializeDumpFunc != nil {
		log.Trace().Str("name", n.Name).Msg("CRIU initialize dump callback")
		var end func()
		ctx, end = profiling.StartTimingCategory(ctx, n.Name)
		defer end()
		err := n.InitializeDumpFunc(ctx, opts)
		if err != nil {
			log.Trace().Err(err).Str("name", n.Name).Msg("CRIU initialize dump callback failed")
			return fmt.Errorf("initialize dump callback: %v", err)
		}
	}
	return nil
}

func (n NotifyCallback) InitializeRestore(ctx context.Context, opts *criu.CriuOpts) error {
	if n.InitializeRestoreFunc != nil {
		log.Trace().Str("name", n.Name).Msg("CRIU initialize restore callback")
		var end func()
		ctx, end = profiling.StartTimingCategory(ctx, n.Name)
		defer end()
		err := n.InitializeRestoreFunc(ctx, opts)
		if err != nil {
			log.Trace().Err(err).Str("name", n.Name).Msg("CRIU initialize restore callback failed")
			return fmt.Errorf("initialize restore callback: %v", err)
		}
	}
	return nil
}

func (n NotifyCallback) FinalizeDump(ctx context.Context, opts *criu.CriuOpts, err error) error {
	if n.FinalizeDumpFunc != nil {
		log.Trace().Str("name", n.Name).Msg("CRIU finalize dump callback")
		var end func()
		ctx, end = profiling.StartTimingCategory(ctx, n.Name)
		defer end()
		err := n.FinalizeDumpFunc(ctx, opts, err)
		if err != nil {
			log.Trace().Err(err).Str("name", n.Name).Msg("CRIU finalize dump callback failed")
			return fmt.Errorf("finalize dump callback: %v", err)
		}
	}
	return nil
}

func (n NotifyCallback) FinalizeRestore(ctx context.Context, opts *criu.CriuOpts, err error) error {
	if n.FinalizeRestoreFunc != nil {
		log.Trace().Str("name", n.Name).Msg("CRIU finalize restore callback")
		var end func()
		ctx, end = profiling.StartTimingCategory(ctx, n.Name)
		defer end()
		err := n.FinalizeRestoreFunc(ctx, opts, err)
		if err != nil {
			log.Trace().Err(err).Str("name", n.Name).Msg("CRIU finalize restore callback failed")
			return fmt.Errorf("finalize restore callback: %v", err)
		}
	}
	return nil
}

func (n NotifyCallback) PreDump(ctx context.Context, opts *criu.CriuOpts) error {
	if n.PreDumpFunc != nil {
		log.Trace().Str("name", n.Name).Msg("CRIU pre-dump callback")
		var end func()
		ctx, end = profiling.StartTimingCategory(ctx, n.Name)
		defer end()
		err := n.PreDumpFunc(ctx, opts)
		if err != nil {
			log.Trace().Err(err).Str("name", n.Name).Msg("CRIU pre-dump callback failed")
			return fmt.Errorf("pre-dump callback: %v", err)
		}
	}
	return nil
}

func (n NotifyCallback) PostDump(ctx context.Context, opts *criu.CriuOpts) error {
	if n.PostDumpFunc != nil {
		log.Trace().Str("name", n.Name).Msg("CRIU post-dump callback")
		var end func()
		ctx, end = profiling.StartTimingCategory(ctx, n.Name)
		defer end()
		err := n.PostDumpFunc(ctx, opts)
		if err != nil {
			log.Trace().Err(err).Str("name", n.Name).Msg("CRIU post-dump callback failed")
			return fmt.Errorf("post-dump callback: %v", err)
		}
	}
	return nil
}

func (n NotifyCallback) PreRestore(ctx context.Context, opts *criu.CriuOpts) error {
	if n.PreRestoreFunc != nil {
		log.Trace().Str("name", n.Name).Msg("CRIU pre-restore callback")
		var end func()
		ctx, end = profiling.StartTimingCategory(ctx, n.Name)
		defer end()
		err := n.PreRestoreFunc(ctx, opts)
		if err != nil {
			log.Trace().Err(err).Str("name", n.Name).Msg("CRIU pre-restore callback failed")
			return fmt.Errorf("pre-restore callback: %v", err)
		}
	}
	return nil
}

func (n NotifyCallback) PreResume(ctx context.Context) error {
	if n.PreResumeFunc != nil {
		log.Trace().Str("name", n.Name).Msg("CRIU pre-resume callback")
		var end func()
		ctx, end = profiling.StartTimingCategory(ctx, n.Name)
		defer end()
		err := n.PreResumeFunc(ctx)
		if err != nil {
			log.Trace().Err(err).Str("name", n.Name).Msg("CRIU pre-resume callback failed")
			return fmt.Errorf("pre-resume callback: %v", err)
		}
	}
	return nil
}

func (n NotifyCallback) PostRestore(ctx context.Context, pid int32) error {
	if n.PostRestoreFunc != nil {
		log.Trace().Int32("pid", pid).Str("name", n.Name).Msg("CRIU post-restore callback")
		var end func()
		ctx, end = profiling.StartTimingCategory(ctx, n.Name)
		defer end()
		err := n.PostRestoreFunc(ctx, pid)
		if err != nil {
			log.Trace().Err(err).Int32("pid", pid).Str("name", n.Name).Msg("CRIU post-restore callback failed")
			return fmt.Errorf("post-restore callback: %v", err)
		}
	}
	return nil
}

func (n NotifyCallback) NetworkLock(ctx context.Context) error {
	if n.NetworkLockFunc != nil {
		log.Trace().Str("name", n.Name).Msg("CRIU network-lock callback")
		var end func()
		ctx, end = profiling.StartTimingCategory(ctx, n.Name)
		defer end()
		err := n.NetworkLockFunc(ctx)
		if err != nil {
			log.Trace().Err(err).Str("name", n.Name).Msg("CRIU network-lock callback failed")
			return fmt.Errorf("network-lock callback: %v", err)
		}
	}
	return nil
}

func (n NotifyCallback) NetworkUnlock(ctx context.Context) error {
	if n.NetworkUnlockFunc != nil {
		log.Trace().Str("name", n.Name).Msg("CRIU network-unlock callback")
		var end func()
		ctx, end = profiling.StartTimingCategory(ctx, n.Name)
		defer end()
		err := n.NetworkUnlockFunc(ctx)
		if err != nil {
			log.Trace().Err(err).Str("name", n.Name).Msg("CRIU network-unlock callback failed")
			return fmt.Errorf("network-unlock callback: %v", err)
		}
	}
	return nil
}

func (n NotifyCallback) SetupNamespaces(ctx context.Context, pid int32) error {
	if n.SetupNamespacesFunc != nil {
		log.Trace().Int32("pid", pid).Str("name", n.Name).Msg("CRIU setup-namespaces callback")
		var end func()
		ctx, end = profiling.StartTimingCategory(ctx, n.Name)
		defer end()
		err := n.SetupNamespacesFunc(ctx, pid)
		if err != nil {
			log.Trace().Err(err).Int32("pid", pid).Str("name", n.Name).Msg("CRIU setup-namespaces callback failed")
			return fmt.Errorf("setup-namespaces callback: %v", err)
		}
	}
	return nil
}

func (n NotifyCallback) PostSetupNamespaces(ctx context.Context) error {
	if n.PostSetupNamespacesFunc != nil {
		log.Trace().Str("name", n.Name).Msg("CRIU post-setup-namespaces callback")
		var end func()
		ctx, end = profiling.StartTimingCategory(ctx, n.Name)
		defer end()
		err := n.PostSetupNamespacesFunc(ctx)
		if err != nil {
			log.Trace().Err(err).Str("name", n.Name).Msg("CRIU post-setup-namespaces callback failed")
			return fmt.Errorf("post-setup-namespaces callback: %v", err)
		}
	}
	return nil
}

func (n NotifyCallback) PostResume(ctx context.Context) error {
	if n.PostResumeFunc != nil {
		log.Trace().Str("name", n.Name).Msg("CRIU post-resume callback")
		var end func()
		ctx, end = profiling.StartTimingCategory(ctx, n.Name)
		defer end()
		err := n.PostResumeFunc(ctx)
		if err != nil {
			log.Trace().Err(err).Str("name", n.Name).Msg("CRIU post-resume callback failed")
			return fmt.Errorf("post-resume callback: %v", err)
		}
	}
	return nil
}

func (n NotifyCallback) OrphanPtsMaster(ctx context.Context, fd int32) error {
	if n.OrphanPtsMasterFunc != nil {
		log.Trace().Int32("fd", fd).Str("name", n.Name).Msg("CRIU orphan-pts-master callback")
		var end func()
		ctx, end = profiling.StartTimingCategory(ctx, n.Name)
		defer end()
		err := n.OrphanPtsMasterFunc(ctx, fd)
		if err != nil {
			log.Trace().Err(err).Int32("fd", fd).Str("name", n.Name).Msg("CRIU orphan-pts-master callback failed")
			return fmt.Errorf("orphan-pts-master callback: %v", err)
		}
	}
	return nil
}

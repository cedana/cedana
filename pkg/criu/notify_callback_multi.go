package criu

// Implements the Notify interface to support registering multiple callbacks, with profiling.
// Callbacks are called in the reverse order they were registered.
// For registration, new callbacks must be appended to the end of the exiting list

import (
	"context"

	"buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
)

type NotifyCallbackMulti struct {
	callbacks []Notify
}

func (n *NotifyCallbackMulti) Include(nfy *NotifyCallback) {
	if nfy == nil {
		return
	}

	n.callbacks = append(n.callbacks, nfy)
}

func (n *NotifyCallbackMulti) IncludeMulti(nfy *NotifyCallbackMulti) {
	if nfy == nil {
		return
	}

	n.callbacks = append(n.callbacks, nfy.callbacks...)
}

func (n NotifyCallbackMulti) Initialize(ctx context.Context, criuPid int32) error {
	for i := len(n.callbacks) - 1; i >= 0; i-- {
		err := n.callbacks[i].Initialize(ctx, criuPid)
		if err != nil {
			return err
		}
	}
	return nil
}

func (n NotifyCallbackMulti) InitializeDump(ctx context.Context, opts *criu.CriuOpts) error {
	for i := len(n.callbacks) - 1; i >= 0; i-- {
		err := n.callbacks[i].InitializeDump(ctx, opts)
		if err != nil {
			return err
		}
	}
	return nil
}

func (n NotifyCallbackMulti) InitializeRestore(ctx context.Context, opts *criu.CriuOpts) error {
	for i := len(n.callbacks) - 1; i >= 0; i-- {
		err := n.callbacks[i].InitializeRestore(ctx, opts)
		if err != nil {
			return err
		}
	}
	return nil
}

func (n NotifyCallbackMulti) FinalizeDump(ctx context.Context, opts *criu.CriuOpts) error {
	for i := len(n.callbacks) - 1; i >= 0; i-- {
		err := n.callbacks[i].FinalizeDump(ctx, opts)
		if err != nil {
			return err
		}
	}
	return nil
}

func (n NotifyCallbackMulti) FinalizeRestore(ctx context.Context, opts *criu.CriuOpts) error {
	for i := len(n.callbacks) - 1; i >= 0; i-- {
		err := n.callbacks[i].FinalizeRestore(ctx, opts)
		if err != nil {
			return err
		}
	}
	return nil
}

func (n NotifyCallbackMulti) PreDump(ctx context.Context, opts *criu.CriuOpts) error {
	for i := len(n.callbacks) - 1; i >= 0; i-- {
		err := n.callbacks[i].PreDump(ctx, opts)
		if err != nil {
			return err
		}
	}
	return nil
}

func (n NotifyCallbackMulti) PostDump(ctx context.Context, opts *criu.CriuOpts) error {
	for i := len(n.callbacks) - 1; i >= 0; i-- {
		err := n.callbacks[i].PostDump(ctx, opts)
		if err != nil {
			return err
		}
	}
	return nil
}

func (n NotifyCallbackMulti) PreRestore(ctx context.Context, opts *criu.CriuOpts) error {
	for i := len(n.callbacks) - 1; i >= 0; i-- {
		err := n.callbacks[i].PreRestore(ctx, opts)
		if err != nil {
			return err
		}
	}
	return nil
}

func (n NotifyCallbackMulti) PreResume(ctx context.Context) error {
	for i := len(n.callbacks) - 1; i >= 0; i-- {
		err := n.callbacks[i].PreResume(ctx)
		if err != nil {
			return err
		}
	}
	return nil
}

func (n NotifyCallbackMulti) PostResume(ctx context.Context) error {
	for i := len(n.callbacks) - 1; i >= 0; i-- {
		err := n.callbacks[i].PostResume(ctx)
		if err != nil {
			return err
		}
	}
	return nil
}

func (n NotifyCallbackMulti) PostRestore(ctx context.Context, pid int32) error {
	for i := len(n.callbacks) - 1; i >= 0; i-- {
		err := n.callbacks[i].PostRestore(ctx, pid)
		if err != nil {
			return err
		}
	}
	return nil
}

func (n NotifyCallbackMulti) NetworkLock(ctx context.Context) error {
	for i := len(n.callbacks) - 1; i >= 0; i-- {
		err := n.callbacks[i].NetworkLock(ctx)
		if err != nil {
			return err
		}
	}
	return nil
}

func (n NotifyCallbackMulti) NetworkUnlock(ctx context.Context) error {
	for i := len(n.callbacks) - 1; i >= 0; i-- {
		err := n.callbacks[i].NetworkUnlock(ctx)
		if err != nil {
			return err
		}
	}
	return nil
}

func (n NotifyCallbackMulti) SetupNamespaces(ctx context.Context, pid int32) error {
	for i := len(n.callbacks) - 1; i >= 0; i-- {
		err := n.callbacks[i].SetupNamespaces(ctx, pid)
		if err != nil {
			return err
		}
	}
	return nil
}

func (n NotifyCallbackMulti) PostSetupNamespaces(ctx context.Context) error {
	for i := len(n.callbacks) - 1; i >= 0; i-- {
		err := n.callbacks[i].PostSetupNamespaces(ctx)
		if err != nil {
			return err
		}
	}
	return nil
}

func (n NotifyCallbackMulti) OrphanPtsMaster(ctx context.Context, fd int32) error {
	for i := len(n.callbacks) - 1; i >= 0; i-- {
		err := n.callbacks[i].OrphanPtsMaster(ctx, fd)
		if err != nil {
			return err
		}
	}
	return nil
}

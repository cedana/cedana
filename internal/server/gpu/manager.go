package gpu

import (
	"context"
	"fmt"

	"github.com/cedana/cedana/pkg/criu"
)

type Manager interface {
	// Attach attaches a GPU controller to a process with the given JID, and PID.
	// Takse in a channel for the PID, allowing this to be called before the process is started,
	// so that the PID can be passed in later.
	Attach(ctx context.Context, lifetime context.Context, jid string, pid <-chan uint32) error

	// AttachAsync calls Attach in background.
	// Returns a channel that will receive an error if the attach fails.
	AttachAsync(ctx context.Context, lifetime context.Context, jid string, pid <-chan uint32) <-chan error

	// IsAttached returns true if GPU is attached to for the given JID.
	IsAttached(jid string) bool

	// Detach detaches the GPU controller from a process with the given JID, and PID.
	Detach(ctx context.Context, jid string) error

	// CRIUCallback returns the CRIU notify callback for GPU C/R.
	CRIUCallback(lifetime context.Context, jid string) *criu.NotifyCallback
}

/////////////////
//// Helpers ////
/////////////////

type ManagerMissing struct{}

func (ManagerMissing) Attach(ctx context.Context, lifetime context.Context, jid string, pid <-chan uint32) error {
	return fmt.Errorf("GPU manager missing")
}

func (ManagerMissing) AttachAsync(ctx context.Context, lifetime context.Context, jid string, pid <-chan uint32) <-chan error {
	err := make(chan error)
	err <- fmt.Errorf("GPU manager missing")
	close(err)
	return err
}

func (ManagerMissing) IsAttached(jid string) bool {
	return false
}

func (ManagerMissing) Detach(ctx context.Context, jid string) error {
	return fmt.Errorf("GPU manager missing")
}

func (ManagerMissing) CRIUCallback(lifetime context.Context, jid string) *criu.NotifyCallback {
	return nil
}

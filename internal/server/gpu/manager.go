package gpu

import (
	"context"
	"fmt"
	"syscall"

	"github.com/cedana/cedana/pkg/criu"
	"github.com/cedana/cedana/pkg/types"
)

type Manager interface {
	// Attach attaches a GPU controller to a process with the given JID, and PID.
	// Takse in a channel for the PID, allowing this to be called before the process is started,
	// so that the PID can be passed in later.
	Attach(ctx context.Context, lifetime context.Context, jid string, user *syscall.Credential, pid <-chan uint32, env []string) error

	// AttachAsync calls Attach in background.
	// Returns a channel that will receive an error if the attach fails.
	AttachAsync(ctx context.Context, lifetime context.Context, jid string, user *syscall.Credential, pid <-chan uint32, env []string) <-chan error

	// IsAttached returns true if GPU is attached to for the given JID.
	IsAttached(jid string) bool

	// Detach detaches the GPU controller from a process with the given JID, and PID.
	Detach(jid string) error

	// Returns server-compatible health checks.
	Checks() types.Checks

	// CRIUCallback returns the CRIU notify callback for GPU C/R.
	CRIUCallback(lifetime context.Context, jid string, user *syscall.Credential, stream int32, env []string) *criu.NotifyCallback
}

/////////////////
//// Helpers ////
/////////////////

// Embed this into unimplmented implmentations
type ManagerMissing struct{}

func (ManagerMissing) Attach(ctx context.Context, lifetime context.Context, jid string, user *syscall.Credential, pid <-chan uint32, env []string) error {
	return fmt.Errorf("GPU manager missing")
}

func (ManagerMissing) AttachAsync(ctx context.Context, lifetime context.Context, jid string, user *syscall.Credential, pid <-chan uint32, env []string) <-chan error {
	err := make(chan error)
	err <- fmt.Errorf("GPU manager missing")
	close(err)
	return err
}

func (ManagerMissing) IsAttached(jid string) bool {
	return false
}

func (ManagerMissing) Detach(jid string) error {
	return fmt.Errorf("GPU manager missing")
}

func (ManagerMissing) CRIUCallback(lifetime context.Context, jid string, user *syscall.Credential, stream int32, env []string) *criu.NotifyCallback {
	return nil
}

func (ManagerMissing) Checks() types.Checks {
	return types.Checks{}
}

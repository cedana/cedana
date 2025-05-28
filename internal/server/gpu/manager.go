package gpu

import (
	"context"
	"fmt"
	"syscall"

	"github.com/cedana/cedana/pkg/criu"
	"github.com/cedana/cedana/pkg/types"
)

type Manager interface {
	// Attach attaches a GPU controller to a process with the given PID.
	// Takes in a channel for the PID, allowing this to be called before the process is started,
	// so that the PID can be passed in later. Returns a unique ID for the GPU controller.
	Attach(ctx context.Context, user *syscall.Credential, pid <-chan uint32, env ...string) (string, error)

	// IsAttached returns true if GPU is attached to a process with the given PID.
	IsAttached(pid uint32) bool

	// Detach detaches the GPU controller from a process with the given and PID.
	Detach(pid uint32) error

	// Returns server-compatible health checks.
	Checks() types.Checks

	// GetID returns the ID of the GPU controller for a given PID.
	GetID(pid uint32) (string, error)

	// CRIUCallback returns the CRIU notify callback for GPU checkpoint/restore.
	CRIUCallback(id string, stream int32, env ...string) *criu.NotifyCallback

	// Sync is used to force the GPU manager to sync its state with the current system state.
	Sync(ctx context.Context) error
}

/////////////////
//// Helpers ////
/////////////////

// Embed this into unimplmented implmentations
type ManagerMissing struct{}

func (ManagerMissing) Attach(ctx context.Context, user *syscall.Credential, pid <-chan uint32, env ...string) (string, error) {
	return "", fmt.Errorf("GPU manager missing")
}

func (ManagerMissing) IsAttached(pid uint32) bool {
	return false
}

func (ManagerMissing) Detach(pid uint32) error {
	return fmt.Errorf("GPU manager missing")
}

func (ManagerMissing) Checks() types.Checks {
	return types.Checks{}
}

func (ManagerMissing) GetID(pid uint32) (string, error) {
	return "", fmt.Errorf("GPU manager missing")
}

func (ManagerMissing) CRIUCallback(id string, stream int32, env ...string) *criu.NotifyCallback {
	return nil
}

func (ManagerMissing) Sync(ctx context.Context) error {
	return fmt.Errorf("GPU manager missing")
}

package gpu

import (
	"context"
	"fmt"

	"github.com/cedana/cedana/pkg/criu"
	"github.com/cedana/cedana/pkg/types"
)

type Manager interface {
	// Attach attaches a GPU controller to a process with the given PID.
	// Takes in a channel for the PID, allowing this to be called before the process is started,
	// so that the PID can be passed in later. Returns a unique ID for the GPU controller.
	Attach(ctx context.Context, pid <-chan uint32) (string, error)

	// IsAttached returns true if GPU is attached to a process with the given PID.
	IsAttached(pid uint32) bool

	// Detach detaches the GPU controller from a process with the given and PID.
	Detach(ctx context.Context, pid uint32) error

	// Returns server-compatible health checks.
	Checks() types.Checks

	// GetID returns the ID of the GPU controller for a given PID.
	GetID(pid uint32) string

	// Freeze the GPU for a given PID
	Freeze(ctx context.Context, pid uint32) error

	// Unfreeze the GPU for a given PID
	Unfreeze(ctx context.Context, pid uint32) error

	// CRIUCallback returns the CRIU notify callback for GPU checkpoint/restore.
	CRIUCallback(id string) *criu.NotifyCallback

	// Sync is used to force the GPU manager to sync its state with the current system state.
	Sync(ctx context.Context) error
}

/////////////////
//// Helpers ////
/////////////////

// Embed this into unimplmented implmentations
type ManagerMissing struct{}

func (ManagerMissing) Attach(ctx context.Context, pid <-chan uint32) (string, error) {
	return "", fmt.Errorf("GPU manager missing")
}

func (ManagerMissing) IsAttached(pid uint32) bool {
	return false
}

func (ManagerMissing) Detach(ctx context.Context, pid uint32) error {
	return fmt.Errorf("GPU manager missing")
}

func (ManagerMissing) Checks() types.Checks {
	return types.Checks{}
}

func (ManagerMissing) GetID(pid uint32) string {
	return ""
}

func (ManagerMissing) CRIUCallback(id string) *criu.NotifyCallback {
	return nil
}

func (ManagerMissing) Freeze(ctx context.Context, pid uint32) error {
	return fmt.Errorf("GPU manager missing")
}

func (ManagerMissing) Unfreeze(ctx context.Context, pid uint32) error {
	return fmt.Errorf("GPU manager missing")
}

func (ManagerMissing) Sync(ctx context.Context) error {
	return fmt.Errorf("GPU manager missing")
}

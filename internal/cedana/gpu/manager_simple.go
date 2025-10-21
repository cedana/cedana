package gpu

import (
	"context"
	"fmt"
	"os"
	"sync"

	"buf.build/gen/go/cedana/cedana-gpu/protocolbuffers/go/gpu"
	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/criu"
	"github.com/cedana/cedana/pkg/plugins"
	"github.com/cedana/cedana/pkg/types"
	"github.com/rs/zerolog/log"
)

// Force the PID to be attached to the GPU controller, even if it's a non-GPU process.
// If false, each GPU controller only appears as busy once the intercepted process makes its first CUDA call.
// Setting this true will force a GPU controller to remain attached to even for non-GPU processes.
const FORCE_ATTACH = true

// Implements a simple GPU manager that spawns GPU controllers on-demand
type ManagerSimple struct {
	controllers pool

	plugins plugins.Manager
	sync    sync.Mutex // Used to prevent concurrent syncs
	syncs   sync.WaitGroup
	wg      *sync.WaitGroup
}

func NewSimpleManager(ctx context.Context, serverWg *sync.WaitGroup, plugins plugins.Manager) (*ManagerSimple, error) {
	manager := &ManagerSimple{
		plugins:     plugins,
		controllers: pool{},
		wg:          serverWg,
	}

	err := manager.Sync(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to sync GPU controllers: %w", err)
	}

	return manager, nil
}

func (m *ManagerSimple) Attach(ctx context.Context, pid <-chan uint32) (id string, err error) {
	// Check if GPU plugin is installed
	var gpuPlugin *plugins.Plugin
	if gpuPlugin = m.plugins.Get("gpu"); !gpuPlugin.IsInstalled() {
		return "", fmt.Errorf("Please install the GPU plugin to use GPU support")
	}
	binary := gpuPlugin.BinaryPaths()[0]

	if _, err := os.Stat(binary); err != nil {
		return "", err
	}

	controller := m.controllers.Book()

	if controller == nil {
		controller, err = m.controllers.Spawn(ctx, binary)
		if err != nil {
			return "", err
		}
	}

	m.wg.Go(func() {
		ok := false
		select {
		case <-ctx.Done():
		case controller.AttachedPID, ok = <-pid:
		}

		if ok && FORCE_ATTACH {
			err = controller.Attach(context.WithoutCancel(ctx), controller.AttachedPID)
			if err != nil {
				log.Debug().Err(err).Str("ID", controller.ID).Uint32("PID", controller.AttachedPID).Msg("failed to forcefully attach GPU controller (FORCE_ATTACH is true)")
				ok = false
			}
		}

		if !ok {
			log.Debug().Str("ID", controller.ID).Msg("GPU attach cancelled")
			m.controllers.Terminate(context.WithoutCancel(ctx), controller.ID)
		} else {
			log.Debug().Str("ID", controller.ID).Uint32("PID", controller.AttachedPID).Msg("attached GPU controller to process")
			controller.Booking.Unlock()
		}
	})

	return controller.ID, nil
}

func (m *ManagerSimple) Detach(ctx context.Context, pid uint32) error {
	controller := m.controllers.Find(pid)
	if controller == nil {
		return fmt.Errorf("no GPU controller found attached to PID %d", pid)
	}
	if acquired, _ := controller.Booking.TryLock(); !acquired {
		return fmt.Errorf("GPU controller attached to PID %d is busy", pid)
	}
	m.controllers.Terminate(ctx, controller.ID)
	return nil
}

func (m *ManagerSimple) IsAttached(pid uint32) bool {
	return m.controllers.Find(pid) != nil
}

func (m *ManagerSimple) GetID(pid uint32) string {
	controller := m.controllers.Find(pid)
	if controller == nil {
		return ""
	}
	return controller.ID
}

func (m *ManagerSimple) Sync(ctx context.Context) error {
	m.syncs.Add(1)
	if !m.sync.TryLock() {
		m.syncs.Done()
		m.syncs.Wait() // Instead of stacking up syncs, just wait for the current one to finish
		return nil
	}
	defer m.syncs.Done()
	defer m.sync.Unlock()

	return m.controllers.Sync(ctx)
}

func (m *ManagerSimple) Checks() types.Checks {
	check := func(ctx context.Context) []*daemon.HealthCheckComponent {
		statusComponent := &daemon.HealthCheckComponent{Name: "status"}

		// Check if GPU plugin is installed
		var gpuPlugin *plugins.Plugin
		if gpuPlugin = m.plugins.Get("gpu"); !gpuPlugin.IsInstalled() {
			statusComponent.Data = "missing"
			statusComponent.Errors = append(statusComponent.Errors, "Please install the GPU plugin to use GPU support.")
			return []*daemon.HealthCheckComponent{statusComponent}
		}
		binary := gpuPlugin.BinaryPaths()[0]
		if _, err := os.Stat(binary); err != nil {
			statusComponent.Data = "invalid"
			statusComponent.Errors = append(statusComponent.Errors, fmt.Sprintf("Invalid binary: %v. Try reinstalling plugin.", err))
			return []*daemon.HealthCheckComponent{statusComponent}
		}

		return m.controllers.Check(binary)(ctx)
	}

	return types.Checks{
		Name: "gpu",
		List: []types.Check{check},
	}
}

func (m *ManagerSimple) CRIUCallback(id string, freezeType ...gpu.FreezeType) *criu.NotifyCallback {
	return m.controllers.CRIUCallback(id, freezeType...)
}

func (m *ManagerSimple) Freeze(ctx context.Context, pid uint32, freezeType ...gpu.FreezeType) error {
	controller := m.controllers.Find(pid)
	if controller == nil {
		return fmt.Errorf("no GPU controller found attached to PID %d", pid)
	}

	freezeType = append(freezeType, CONTROLLER_DEFAULT_FREEZE_TYPE)

	_, err := controller.Freeze(ctx, &gpu.FreezeReq{Type: freezeType[0]})
	return err
}

func (m *ManagerSimple) Unfreeze(ctx context.Context, pid uint32) error {
	controller := m.controllers.Find(pid)
	if controller == nil {
		return fmt.Errorf("no GPU controller found attached to PID %d", pid)
	}

	_, err := controller.Unfreeze(ctx, &gpu.UnfreezeReq{})
	return err
}

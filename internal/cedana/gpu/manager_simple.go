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

	var spawnedNew bool

	controller := m.controllers.Book()

	if controller == nil {
		log.Debug().Msg("spawning a new GPU controller")
		controller, err = m.controllers.Spawn(ctx, binary)
		if err != nil {
			return "", err
		}
		spawnedNew = true
	} else {
		log.Debug().Str("ID", controller.ID).Msg("booking free GPU controller")
	}

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		defer controller.Booking.Unlock()
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
			log.Debug().Err(ctx.Err()).Str("ID", controller.ID).Msg("terminating GPU controller")
			if spawnedNew {
				m.controllers.Terminate(controller.ID)
			}
		} else {
			log.Debug().Str("ID", controller.ID).Uint32("PID", controller.AttachedPID).Msg("attached GPU controller to process")
		}
	}()

	return controller.ID, nil
}

func (m *ManagerSimple) Detach(pid uint32) error {
	log.Debug().Uint32("PID", pid).Msg("detaching GPU controller from process")
	controller := m.controllers.Find(pid)
	if controller == nil {
		log.Debug().Uint32("PID", pid).Msg("no GPU controller found attached to process")
		return fmt.Errorf("no GPU controller found attached to PID %d", pid)
	}
	m.controllers.Terminate(controller.ID)
	return nil
}

func (m *ManagerSimple) IsAttached(pid uint32) bool {
	return m.controllers.Find(pid) != nil
}

func (m *ManagerSimple) GetID(pid uint32) (string, error) {
	controller := m.controllers.Find(pid)
	if controller == nil {
		return "", fmt.Errorf("no GPU controller found attached to PID %d", pid)
	}
	return controller.ID, nil
}

func (m *ManagerSimple) Sync(ctx context.Context) error {
	m.sync.Lock()
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

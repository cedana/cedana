package gpu

import (
	"context"
	"fmt"
	"os"

	"buf.build/gen/go/cedana/cedana-gpu/protocolbuffers/go/gpu"
	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/internal/db"
	"github.com/cedana/cedana/pkg/criu"
	"github.com/cedana/cedana/pkg/plugins"
	"github.com/cedana/cedana/pkg/types"
	"github.com/rs/zerolog/log"
)

// Implements a simple GPU manager that spawns GPU controllers on-demand

type ManagerSimple struct {
	controllers pool

	plugins plugins.Manager
	db      db.GPU
}

func NewSimpleManager(ctx context.Context, plugins plugins.Manager, db db.GPU) (*ManagerSimple, error) {
	manager := &ManagerSimple{
		plugins:     plugins,
		db:          db,
		controllers: pool{},
	}

	err := manager.Sync(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to sync GPU controllers: %w", err)
	}

	return manager, nil
}

func (m *ManagerSimple) Attach(ctx context.Context, multiprocessType gpu.FreezeType, pid <-chan uint32) (id string, err error) {
	// Check if GPU plugin is installed
	var gpuPlugin *plugins.Plugin
	if gpuPlugin = m.plugins.Get("gpu"); !gpuPlugin.IsInstalled() {
		return "", fmt.Errorf("Please install the GPU plugin to use GPU support")
	}
	binary := gpuPlugin.BinaryPaths()[0]

	if _, err := os.Stat(binary); err != nil {
		return "", err
	}

	controller := m.controllers.GetFree()

	if controller == nil {
		log.Debug().Msg("spawning a new GPU controller")
		controller, err = m.controllers.Spawn(binary)
		if err != nil {
			return "", err
		}
	}

	controller.FreezeType = multiprocessType

	log.Debug().Str("ID", controller.ID).Msg("connecting to GPU controller")

	defer func() {
		if err != nil {
			controller.Terminate()
		}
	}()

	err = controller.Connect(ctx)
	if err != nil {
		return "", err
	}

	log.Debug().Str("ID", controller.ID).Str("Address", controller.Target()).Msg("connected to GPU controller")

	go func() {
		defer controller.PendingAttach.Store(false)
		ok := false
		select {
		case <-ctx.Done():
		case controller.AttachedPID, ok = <-pid:
		}
		if !ok {
			log.Debug().Err(ctx.Err()).Str("ID", controller.ID).Msg("terminating GPU controller")
			controller.Terminate()
			err := m.db.DeleteGPUController(ctx, controller.ID)
			if err != nil {
				log.Warn().Err(err).Str("ID", controller.ID).Msg("failed to delete GPU controller from db, should get cleaned up eventually")
			}
		} else {
			log.Debug().Str("ID", controller.ID).Uint32("PID", controller.AttachedPID).Msg("attached GPU controller to process")
			err = m.db.PutGPUController(ctx, &db.GPUController{
				ID:          controller.ID,
				PID:         uint32(controller.Process.Pid),
				FreezeType:  controller.FreezeType,
				Address:     controller.Target(),
				AttachedPID: controller.AttachedPID,
			})
			if err != nil {
				log.Error().Err(err).Str("ID", controller.ID).Msg("failed to update GPU controller in db, terminating to maintain consistency")
				controller.Terminate()
			}
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
	controller.Terminate()
	return m.db.DeleteGPUController(context.Background(), controller.ID)
}

func (m *ManagerSimple) IsAttached(pid uint32) bool {
	return m.controllers.Find(pid) != nil
}

func (m *ManagerSimple) MultiprocessType(pid uint32) gpu.FreezeType {
	controller := m.controllers.Find(pid)
	if controller == nil {
		return gpu.FreezeType_FREEZE_TYPE_IPC
	}
	return controller.FreezeType
}

func (m *ManagerSimple) GetID(pid uint32) (string, error) {
	controller := m.controllers.Find(pid)
	if controller == nil {
		return "", fmt.Errorf("no GPU controller found attached to PID %d", pid)
	}
	return controller.ID, nil
}

func (m *ManagerSimple) Sync(ctx context.Context) error {
	dbControllers, err := m.db.ListGPUControllers(ctx)
	if err != nil {
		return err
	}
	for _, dbController := range dbControllers {
		if existing := m.controllers.Get(dbController.ID); existing != nil {
			existing.AttachedPID = dbController.AttachedPID
			continue
		}

		err = m.controllers.Import(ctx, dbController)
		if err != nil {
			// If import fails, we assume the controller is no longer running
			log.Debug().Str("reason", err.Error()).Str("ID", dbController.ID).Msg("clearing stale GPU controller in DB")
			m.db.DeleteGPUController(ctx, dbController.ID)
		}
	}

	return nil
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

func (m *ManagerSimple) CRIUCallback(id string) *criu.NotifyCallback {
	return m.controllers.CRIUCallback(id)
}

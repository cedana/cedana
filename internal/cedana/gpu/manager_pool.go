package gpu

// Implements a GPU manager pool that is capable of maintaining a pool of GPU controllers

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"buf.build/gen/go/cedana/cedana-gpu/protocolbuffers/go/gpu"
	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/internal/db"
	"github.com/cedana/cedana/pkg/criu"
	"github.com/cedana/cedana/pkg/plugins"
	"github.com/cedana/cedana/pkg/types"
	"github.com/rs/zerolog/log"
)

const (
	DB_SYNC_INTERVAL       = 10 * time.Second
	DB_SYNC_RETRY_INTERVAL = 1 * time.Second

	FREEZE_TIMEOUT   = 20 * time.Second
	UNFREEZE_TIMEOUT = 20 * time.Second
	DUMP_TIMEOUT     = 5 * time.Minute
	RESTORE_TIMEOUT  = 5 * time.Minute
	HEALTH_TIMEOUT   = 30 * time.Second
)

type ManagerPool struct {
	controllers pool

	poolSize int

	plugins plugins.Manager
	db      db.GPU
	pending chan action
	sync    sync.Mutex // Used to prevent concurrent syncs

	wg *sync.WaitGroup
}

type actionType int

const (
	maintainPool actionType = iota
	putController
	shutdownPool
	shutdown
)

type action struct {
	typ actionType
	id  string
}

func NewPoolManager(lifetime context.Context, serverWg *sync.WaitGroup, poolSize int, plugins plugins.Manager, db db.GPU) (*ManagerPool, error) {
	manager := &ManagerPool{
		pending:     make(chan action, 64),
		poolSize:    poolSize,
		plugins:     plugins,
		wg:          &sync.WaitGroup{},
		db:          db,
		controllers: pool{},
	}

	err := manager.Sync(lifetime)
	if err != nil {
		return nil, err
	}

	// Spawn a background routine that will keep the DB in sync
	// with retry logic. Can extend to use a backoff strategy.
	serverWg.Add(1)
	go func() {
		defer serverWg.Done()
		for {
			select {
			case <-lifetime.Done():
				log.Info().Msg("syncing GPU manager with DB before shutdown")
				var errs []error
				var failedActions []action
				manager.wg.Wait() // wait for all background routines
				manager.pending <- action{shutdownPool, ""}
				manager.pending <- action{shutdown, ""}
				for action := range manager.pending {
					if action.typ == shutdown {
						break
					}
					ctx := context.WithoutCancel(lifetime)
					err := manager.syncWithDB(ctx, action)
					if err != nil {
						errs = append(errs, err)
						failedActions = append(failedActions, action)
					}
				}
				err = errors.Join(errs...)
				if err != nil {
					log.Error().Msg("failed to sync GPU manager with DB before shutdown")
					for i, action := range failedActions {
						log.Debug().Err(errs[i]).Str("id", action.id).Str("type", action.typ.String()).Send()
					}
				}
				return
			case action := <-manager.pending:
				err := manager.syncWithDB(lifetime, action)
				if err != nil {
					manager.pending <- action
					log.Debug().Err(err).Str("id", action.id).Str("type", action.typ.String()).Msg("GPU manager DB sync failed, retrying...")
					time.Sleep(DB_SYNC_RETRY_INTERVAL)
				}

			case <-time.After(DB_SYNC_INTERVAL):
				manager.pending <- action{maintainPool, ""}
			}
		}
	}()

	return manager, nil
}

func (m *ManagerPool) Attach(ctx context.Context, multiprocessType gpu.FreezeType, pid <-chan uint32) (id string, err error) {
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
			m.controllers.Delete(controller.ID)
			m.pending <- action{putController, controller.ID}
		}
	}()

	err = controller.Connect(ctx)
	if err != nil {
		return "", err
	}

	log.Debug().Str("ID", controller.ID).Str("Address", controller.Target()).Msg("connected to GPU controller")

	m.pending <- action{putController, controller.ID}

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		defer controller.PendingAttach.Store(false)
		ok := false
		select {
		case <-ctx.Done():
		case controller.AttachedPID, ok = <-pid:
		}
		if !ok {
			log.Debug().Err(ctx.Err()).Str("ID", controller.ID).Msg("terminating GPU controller")
			controller.Terminate()
			m.controllers.Delete(controller.ID)
		} else {
			log.Debug().Str("ID", controller.ID).Uint32("PID", controller.AttachedPID).Msg("attached GPU controller to process")
		}
		m.pending <- action{putController, controller.ID}
	}()

	return controller.ID, nil
}

func (m *ManagerPool) Detach(pid uint32) error {
	log.Debug().Uint32("PID", pid).Msg("detaching GPU controller from process")
	controller := m.controllers.Find(pid)
	if controller == nil {
		log.Debug().Uint32("PID", pid).Msg("no GPU controller found attached to process")
		return fmt.Errorf("no GPU controller found attached to PID %d", pid)
	}
	controller.Terminate()
	m.controllers.Delete(controller.ID)
	m.pending <- action{putController, controller.ID}
	return nil
}

func (m *ManagerPool) IsAttached(pid uint32) bool {
	return m.controllers.Find(pid) != nil
}

func (m *ManagerPool) MultiprocessType(pid uint32) gpu.FreezeType {
	controller := m.controllers.Find(pid)
	if controller == nil {
		return gpu.FreezeType_FREEZE_TYPE_IPC
	}
	return controller.FreezeType
}

func (m *ManagerPool) GetID(pid uint32) (string, error) {
	controller := m.controllers.Find(pid)
	if controller == nil {
		return "", fmt.Errorf("no GPU controller found attached to PID %d", pid)
	}
	return controller.ID, nil
}

func (m *ManagerPool) Sync(ctx context.Context) error {
	return m.syncWithDB(ctx, action{maintainPool, ""})
}

func (m *ManagerPool) Checks() types.Checks {
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

func (m *ManagerPool) CRIUCallback(id string) *criu.NotifyCallback {
	return m.controllers.CRIUCallback(id)
}

////////////////////////
//// Helper Methods ////
////////////////////////

func (i actionType) String() string {
	return [...]string{"maintainPool", "putController", "shutdownPool", "shutdown"}[i]
}

func (m *ManagerPool) syncWithDB(ctx context.Context, a action) error {
	m.sync.Lock()
	defer m.sync.Unlock()
	typ := a.typ

	var err error

	switch typ {
	case maintainPool:
		// For this action we never return an error, as its runs on an interval
		// so we hope the action will succeed eventually

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
				m.pending <- action{putController, dbController.ID}
			}
		}

		free, pending, busy, remaining := m.controllers.List()

		log.Trace().Int("free", len(free)).Int("pending", len(pending)).Int("busy", len(busy)).Int("target", m.poolSize).Msg("GPU controller pool")

		// Remove controllers not in either free or busy list

		for _, controller := range remaining {
			log.Debug().Str("ID", controller.ID).Msg("clearing stale GPU controller in pool")
			controller.Terminate()
			m.controllers.Delete(controller.ID)
			m.pending <- action{putController, controller.ID}
		}

		// Maintain the pool size

		if len(free) < m.poolSize {
			log.Debug().Int("target", m.poolSize).Int("current", len(free)).Msg("maintaining GPU pool size")
			for i := len(free); i < m.poolSize; i++ {
				controller, err := m.controllers.Spawn(m.plugins.Get("gpu").BinaryPaths()[0])
				if err != nil {
					log.Debug().Err(err).Msg("failed to spawn GPU controller to maintain pool size")
					// Stop trying to spawn more right now as it could be an OOM condition
					return nil
				}
				log.Debug().Str("ID", controller.ID).Msg("spawned GPU controller to maintain pool size")
				controller.PendingAttach.Store(false)
				m.pending <- action{putController, controller.ID}
			}
		} else if len(free) > m.poolSize {
			log.Debug().Int("target", m.poolSize).Int("current", len(free)).Msg("reducing GPU pool size")
			for i := len(free); i > m.poolSize; i-- {
				controller := m.controllers.GetFree()
				log.Debug().Str("ID", controller.ID).Msg("terminating GPU controller to reduce pool size")
				controller.Terminate()
				m.controllers.Delete(controller.ID)
				m.pending <- action{putController, controller.ID}
			}
		}

	case shutdownPool:
		// This action is used to shutdown the pool and terminate all free controllers

		free, _, _, _ := m.controllers.List()

		for _, controller := range free {
			log.Debug().Str("ID", controller.ID).Msg("terminating free GPU controller")
			controller.Terminate()
			m.controllers.Delete(controller.ID)
			m.pending <- action{putController, controller.ID}
		}

	case putController:
		id := a.id
		controller := m.controllers.Get(id)
		if controller == nil {
			err = m.db.DeleteGPUController(ctx, id)
		} else {
			var address string
			if controller.ClientConn != nil {
				address = controller.ClientConn.Target()
			}
			err = m.db.PutGPUController(ctx, &db.GPUController{
				ID:          controller.ID,
				PID:         uint32(controller.Process.Pid),
				Address:     address,
				AttachedPID: controller.AttachedPID,
				FreezeType:  controller.FreezeType,
			})
		}
	}
	return err
}

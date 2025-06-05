package gpu

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"buf.build/gen/go/cedana/cedana-gpu/protocolbuffers/go/gpu"
	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/criu"
	"github.com/cedana/cedana/pkg/plugins"
	"github.com/cedana/cedana/pkg/types"
	"github.com/rs/zerolog/log"
)

const SYNC_INTERVAL = 10 * time.Second

// Implements a GPU manager pool that is capable of maintaining a pool of GPU controllers
type ManagerPool struct {
	*ManagerSimple // Embed simple manager implements most of what we need
	poolSize       int

	plugins plugins.Manager
}

func NewPoolManager(lifetime context.Context, serverWg *sync.WaitGroup, poolSize int, plugins plugins.Manager) (*ManagerPool, error) {
	simpleManager, err := NewSimpleManager(lifetime, serverWg, plugins)
	if err != nil {
		return nil, err
	}

	manager := &ManagerPool{
		poolSize:      poolSize,
		plugins:       plugins,
		ManagerSimple: simpleManager, // Embed the simple manager
	}

	err = manager.Sync(lifetime) // Initial sync to populate the pool
	if err != nil {
		return nil, fmt.Errorf("failed to sync GPU controllers: %w", err)
	}

	// Spawn a background routine that will keep the DB in sync
	// with retry logic. Can extend to use a backoff strategy.
	serverWg.Add(1)
	go func() {
		defer serverWg.Done()
		for {
			select {
			case <-lifetime.Done():
				manager.Shutdown(context.WithoutCancel(lifetime))
				return
			case <-time.After(SYNC_INTERVAL):
				err := manager.Sync(lifetime)
				if err != nil {
					log.Error().Err(err).Dur("interval", SYNC_INTERVAL).Msg("failed to sync GPU controllers in background, will retry...")
				}
			}
		}
	}()

	return manager, nil
}

func (m *ManagerPool) Sync(ctx context.Context) error {
	err := m.ManagerSimple.Sync(ctx) // Call the sync method of the embedded simple manager
	if err != nil {
		return fmt.Errorf("failed to sync GPU controllers: %w", err)
	}

	m.sync.Lock()
	defer m.sync.Unlock()

	free, busy, remaining := m.controllers.List()

	log.Debug().Int("free", len(free)).Int("busy", len(busy)).Int("target", m.poolSize).Msg("GPU controller pool")

	// Remove controllers not in either free or busy list

	for _, controller := range remaining {
		log.Debug().Str("ID", controller.ID).Msg("clearing stale GPU controller in pool")
		m.controllers.Terminate(controller.ID)
	}

	// Maintain the pool size

	if len(free) < m.poolSize {
		log.Debug().Int("target", m.poolSize).Int("current", len(free)).Msg("maintaining GPU pool size")
		for i := len(free); i < m.poolSize; i++ {
			wg := &sync.WaitGroup{}
			wg.Add(1)
			go func() {
				defer wg.Done()
				controller, err := m.controllers.Spawn(ctx, m.plugins.Get("gpu").BinaryPaths()[0])
				if err != nil {
					log.Debug().Err(err).Msg("failed to spawn GPU controller to maintain pool size")
					return
				}
				log.Debug().Str("ID", controller.ID).Msg("spawned GPU controller to maintain pool size")
				controller.Busy.Store(false)
			}()
			wg.Wait()
		}
	} else if len(free) > m.poolSize {
		log.Debug().Int("target", m.poolSize).Int("current", len(free)).Msg("reducing GPU pool size")
		for _, controller := range free[m.poolSize:] {
			// Ensure we only terminate controllers that are still free
			if controller.Busy.CompareAndSwap(false, true) {
				log.Debug().Str("ID", controller.ID).Msg("terminating GPU controller to reduce pool size")
				m.controllers.Terminate(controller.ID)
			}
		}
	}
	return nil
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

func (m *ManagerPool) CRIUCallback(id string, freezeType ...gpu.FreezeType) *criu.NotifyCallback {
	return m.controllers.CRIUCallback(id, freezeType...)
}

////////////////////////
//// Helper Methods ////
////////////////////////

func (m *ManagerPool) Shutdown(ctx context.Context) {
	m.ManagerSimple.Sync(ctx) // Call the sync method of the embedded simple manager

	m.sync.Lock()
	defer m.sync.Unlock()

	// This action is used to shutdown the pool and terminate all free controllers

	free, _, _ := m.controllers.List()

	for _, controller := range free {
		// Ensure we only terminate controllers that are still free
		if controller.Busy.CompareAndSwap(false, true) {
			log.Debug().Str("ID", controller.ID).Msg("terminating free GPU controller")
			m.controllers.Terminate(controller.ID)
		}
	}
}

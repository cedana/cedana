package gpu

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/cedana/cedana/pkg/plugins"
	"github.com/rs/zerolog/log"
)

const SYNC_INTERVAL = 10 * time.Second

// Implements a GPU manager pool that is capable of maintaining a pool of GPU controllers
type ManagerPool struct {
	*ManagerSimple // Embed simple manager implements most of what we need

	poolSize int
}

func NewPoolManager(lifetime context.Context, serverWg *sync.WaitGroup, poolSize int, plugins plugins.Manager) (*ManagerPool, error) {
	simpleManager, err := NewSimpleManager(lifetime, serverWg, plugins)
	if err != nil {
		return nil, err
	}

	manager := &ManagerPool{
		poolSize:      poolSize,
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
				manager.poolSize = 0 // Reset it so all free controllers are terminated
				err := manager.Sync(context.WithoutCancel(lifetime))
				if err != nil {
					log.Error().Err(err).Msg("failed to sync GPU controllers on shutdown")
				}
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
	m.sync.Lock()
	defer m.sync.Unlock()

	err := m.controllers.Sync(ctx)
	if err != nil {
		return fmt.Errorf("failed to sync GPU controllers: %w", err)
	}

	free, busy, remaining := m.controllers.List()

	log.Debug().Int("free", len(free)).Int("busy", len(busy)).Int("target", m.poolSize).Msg("GPU controller pool")

	// Remove controllers not in either free or busy list

	for _, controller := range remaining {
		log.Debug().Str("ID", controller.ID).Msg("clearing stale GPU controller in pool")
		m.controllers.Terminate(controller.ID)
	}

	// Maintain the pool size

	if len(free) < m.poolSize {
		log.Debug().Int("target", m.poolSize).Int("free", len(free)).Msg("maintaining GPU pool size")
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
				controller.Booking.Unlock()
			}()
			wg.Wait()
		}
	} else if len(free) > m.poolSize {
		log.Debug().Int("target", m.poolSize).Int("free", len(free)).Msg("reducing GPU pool size")
		for _, controller := range free[m.poolSize:] {
			// Ensure we only terminate controllers that are still free
			if acquired, _ := controller.Booking.TryLock(); acquired {
				log.Debug().Str("ID", controller.ID).Msg("terminating GPU controller to reduce pool size")
				m.controllers.Terminate(controller.ID)
			}
		}
	}
	return nil
}

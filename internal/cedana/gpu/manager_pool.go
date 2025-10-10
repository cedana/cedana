package gpu

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/cedana/cedana/pkg/config"
	"github.com/cedana/cedana/pkg/plugins"
	"github.com/rs/zerolog/log"
)

const (
	SYNC_INTERVAL         = 10 * time.Second
	SYNC_SHUTDOWN_TIMEOUT = 45 * time.Second
)

// Implements a GPU manager pool that is capable of maintaining a pool of GPU controllers
type ManagerPool struct {
	*ManagerSimple // Embed simple manager implements most of what we need

	poolSize int
	free     int
	busy     int
	stale    int
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
	serverWg.Go(func() {
		for {
			select {
			case <-lifetime.Done():
				log.Info().Msg("syncing GPU manager before shutdown")
				ctx, cancel := context.WithTimeout(context.WithoutCancel(lifetime), SYNC_SHUTDOWN_TIMEOUT)
				defer cancel()
				for {
					select {
					case <-ctx.Done():
						log.Warn().Msg("timeout reached while syncing GPU manager on shutdown")
						return
					default:
						manager.poolSize = 0 // Reset it so all free controllers are terminated
						err := manager.Sync(ctx)
						if err != nil {
							log.Error().Err(err).Msg("failed to sync GPU controllers on shutdown")
						}
						if manager.free == 0 && manager.stale == 0 {
							return
						}
						time.Sleep(1 * time.Second) // Wait a bit before retrying
					}
				}
			case <-time.After(SYNC_INTERVAL):
				err := manager.Sync(lifetime)
				if err != nil {
					log.Error().Err(err).Dur("interval", SYNC_INTERVAL).Msg("failed to sync GPU controllers in background, will retry...")
				}
			}
		}
	})

	return manager, nil
}

func (m *ManagerPool) Sync(ctx context.Context) error {
	m.syncs.Add(1)
	if !m.sync.TryLock() {
		m.syncs.Done()
		m.syncs.Wait() // Instead of stacking up syncs, just wait for the current one to finish
		return nil
	}
	defer m.syncs.Done()
	defer m.sync.Unlock()

	err := m.controllers.Sync(ctx)
	if err != nil {
		return fmt.Errorf("failed to sync GPU controllers: %w", err)
	}

	free, busy, remaining, remainingReason := m.controllers.List()

	m.free = len(free)
	m.busy = len(busy)
	m.stale = len(remaining)

	log.Debug().
		Int("free", m.free).
		Int("busy", m.busy).
		Int("target", m.poolSize).
		Int("stale", m.stale).
		Msg("GPU controller pool")

	if config.Global.GPU.Debug {
		log.Warn().Msg("GPU controller pool is in debug mode, not maintaining pool size")
		return nil // Allow external maintenance of pool for debugging
	}

	// Remove controllers not in either free or busy list

	for i, controller := range remaining {
		if acquired, _ := controller.Booking.TryLock(); acquired {
			log.Debug().Str("ID", controller.ID).Str("reason", remainingReason[i]).Msg("clearing stale GPU controller in pool")
			m.controllers.Terminate(ctx, controller.ID)
		}
	}

	// Maintain the pool size

	if len(free) < m.poolSize {
		log.Debug().Int("target", m.poolSize).Int("free", len(free)).Msg("maintaining GPU pool size")
		wg := &sync.WaitGroup{}
		for i := len(free); i < m.poolSize; i++ {
			wg.Go(func() {
				controller, err := m.controllers.Spawn(ctx, m.plugins.Get("gpu").BinaryPaths()[0])
				if err != nil {
					log.Debug().Err(err).Msg("failed to spawn GPU controller to maintain pool size")
					return
				}
				controller.Booking.Unlock()
			})
		}
		wg.Wait()
	} else if len(free) > m.poolSize {
		log.Debug().Int("target", m.poolSize).Int("free", len(free)).Msg("reducing GPU pool size")
		wg := &sync.WaitGroup{}
		for _, controller := range free[m.poolSize:] {
			wg.Go(func() {
				if controller.Book() { // Only terminate if controller is still free
					m.controllers.Terminate(ctx, controller.ID)
				} else {
					log.Debug().Str("ID", controller.ID).Msg("skipping termination of busy GPU controller")
				}
			})
		}
		wg.Wait()
	}

	return nil
}

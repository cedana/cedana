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
	HELD_WARN_AFTER       = 1 * time.Minute // Warn when a stale controller stays held longer
)

// Implements a GPU manager pool that is capable of maintaining a pool of GPU controllers
type ManagerPool struct {
	*ManagerSimple // Embed simple manager implements most of what we need

	poolSize int
	free     int
	busy     int
	stale    int
	held     map[string]*heldController // Skipped stale controllers, by ID
}

// A stale controller that a dump or a restore holds
type heldController struct {
	since  time.Time // First skip
	warned bool
}

func NewPoolManager(lifetime context.Context, serverWg *sync.WaitGroup, poolSize int, plugins plugins.Manager) (*ManagerPool, error) {
	simpleManager, err := newSimpleManager(lifetime, serverWg, plugins, true) // GPU logs are the daemon's to ship
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
						if (manager.free == 0 && manager.stale == 0) || config.Global.GPU.Debug {
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
	done, err := m.waitOrStartSync(ctx)
	if err != nil || done == nil {
		return err
	}
	defer done()

	err = m.controllers.Sync(ctx)
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
		log.Warn().Msg("GPU controller pool is in debug mode, not maintaining pool size, you may spawn a GPU controller manually")
		return nil // Allow external maintenance of pool for debugging
	}

	// Remove controllers not in either free or busy list

	held := make(map[string]*heldController)
	defer func() { m.held = held }()

	for i, controller := range remaining {
		acquired, err := controller.Booking.TryLock()
		if err != nil {
			log.Debug().Err(err).Str("ID", controller.ID).Str("reason", remainingReason[i]).Msg("failed to lock stale GPU controller for clearing")
		}
		if acquired {
			// Re-check under the lock: whoever booked the controller attaches it before releasing the
			// lock, so it may have been attached since it was synced above.
			if controller.Sync(ctx, false) == nil {
				controller.syncFails = 0
				if status, _ := controller.Status(); status != CONTROLLER_STALE {
					controller.Booking.Unlock()
					continue
				}
			}
			log.Debug().Str("ID", controller.ID).Str("reason", remainingReason[i]).Msg("clearing stale GPU controller in pool")
			if !m.controllers.TryTerminate(ctx, controller.ID) {
				// A dump or a restore holds it. Try again at the next sync
				controller.Booking.Unlock()
				held[controller.ID] = m.skipHeld(controller.ID, remainingReason[i])
				continue
			}
			// A controller found on disk has no ErrBuf
			if controller.ErrBuf != nil && controller.ErrBuf.Len() > 0 {
				log.Error().Str("ID", controller.ID).Str("reason", remainingReason[i]).Str("stderr", controller.ErrBuf.String()).Msg("stale GPU controller captured stderr")
			}
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

// Logs the skip. Warns once after HELD_WARN_AFTER
func (m *ManagerPool) skipHeld(id string, reason string) *heldController {
	h := m.held[id]
	if h == nil {
		h = &heldController{since: time.Now()}
	}
	held := time.Since(h.since)

	log.Debug().Str("ID", id).Str("reason", reason).Dur("held", held).Msg("skipping stale GPU controller held by a dump or restore")
	if held > HELD_WARN_AFTER && !h.warned {
		log.Warn().Str("ID", id).Str("reason", reason).Dur("held", held).Msg("stale GPU controller held by a dump or restore for more than 1 min")
		h.warned = true
	}
	return h
}

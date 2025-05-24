package gpu

// Implements a GPU manager pool that is capable of maintaining a pool of GPU controllers

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"sync"
	"syscall"
	"time"

	"buf.build/gen/go/cedana/cedana-gpu/protocolbuffers/go/gpu"
	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/internal/db"
	criu_client "github.com/cedana/cedana/pkg/criu"
	"github.com/cedana/cedana/pkg/plugins"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/pkg/utils"
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
	controllers controllers
	poolSize    int

	plugins plugins.Manager
	db      db.GPU
	pending chan action

	wg *sync.WaitGroup
}

type actionType int

const (
	maintainPool actionType = iota
	putController
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
		controllers: controllers{},
	}

	err := manager.syncWithDB(lifetime, action{maintainPool, ""})
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

func (m *ManagerPool) Attach(ctx context.Context, user *syscall.Credential, pid <-chan uint32, env ...string) (id string,
	err error,
) {
	// Check if GPU plugin is installed
	var gpuPlugin *plugins.Plugin
	if gpuPlugin = m.plugins.Get("gpu"); !gpuPlugin.IsInstalled() {
		return "", fmt.Errorf("Please install the GPU plugin to use GPU support")
	}
	binary := gpuPlugin.BinaryPaths()[0]

	if _, err := os.Stat(binary); err != nil {
		return "", err
	}

	var controller *controller

	freeList := m.controllers.FreeList()

	if len(freeList) > 0 {
		controller = freeList[0]
		log.Debug().Str("ID", controller.ID).Msg("using existing GPU controller in pool")
	} else {
		log.Debug().Msg("spawning a new GPU controller")
		controller, err = m.controllers.Spawn(binary, user, env...)
		if err != nil {
			return "", err
		}
	}

	log.Debug().Str("ID", controller.ID).Msg("connecting to GPU controller")

	err = controller.Connect(ctx, m.wg)
	if err != nil {
		return "", err
	}

	log.Debug().Str("ID", controller.ID).Str("Address", controller.Target()).Msg("connected to GPU controller")

	m.pending <- action{putController, controller.ID}

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
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

func (m *ManagerPool) GetID(pid uint32) (string, error) {
	controller := m.controllers.Find(pid)
	if controller == nil {
		return "", fmt.Errorf("no GPU controller found attached to PID %d", pid)
	}
	return controller.ID, nil
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

		user, err := utils.GetCredentials()
		if err != nil {
			statusComponent.Data = "failed"
			statusComponent.Errors = append(statusComponent.Errors, fmt.Sprintf("Failed to get user credentials: %v", err))
			return []*daemon.HealthCheckComponent{statusComponent}
		}

		// Spawn a new GPU controller

		controller, err := m.controllers.Spawn(binary, user)
		defer controller.Terminate()
		defer m.controllers.Delete(controller.ID)
		if err != nil {
			statusComponent.Data = "failed"
			statusComponent.Errors = append(statusComponent.Errors, fmt.Sprintf("Failed controller spawn: %v", err))
			return []*daemon.HealthCheckComponent{statusComponent}
		}

		err = controller.Connect(ctx, m.wg)
		if err != nil {
			statusComponent.Data = "failed"
			statusComponent.Errors = append(statusComponent.Errors, fmt.Sprintf("Failed controller connect: %v", err))
			return []*daemon.HealthCheckComponent{statusComponent}
		}

		components, err := controller.WaitForHealthCheck(ctx, m.wg)
		if components == nil && err != nil {
			statusComponent.Data = "failed"
			statusComponent.Errors = append(statusComponent.Errors, fmt.Sprintf("Failed controller health check: %v", err))
			return []*daemon.HealthCheckComponent{statusComponent}
		}

		statusComponent.Data = "available"

		return append([]*daemon.HealthCheckComponent{statusComponent}, components...)
	}

	return types.Checks{
		Name: "gpu",
		List: []types.Check{check},
	}
}

func (m *ManagerPool) CRIUCallback(id string, stream int32, env ...string) *criu_client.NotifyCallback {
	callback := &criu_client.NotifyCallback{Name: "gpu"}

	// Add pre-dump hook for GPU dump. We freeze the GPU controller so we can
	// do the GPU dump in parallel to CRIU dump.
	dumpErr := make(chan error, 1)
	callback.PreDumpFunc = func(ctx context.Context, opts *criu_proto.CriuOpts) error {
		waitCtx, cancel := context.WithTimeout(ctx, FREEZE_TIMEOUT)
		defer cancel()

		pid := uint32(opts.GetPid())

		controller := m.controllers.Get(id)
		if controller == nil {
			return fmt.Errorf("GPU controller not found, is the process still running?")
		}

		_, err := controller.Freeze(waitCtx, &gpu.FreezeReq{})
		if err != nil {
			log.Error().Err(err).Str("ID", id).Uint32("PID", pid).Msg("failed to freeze GPU")
			return fmt.Errorf("failed to freeze GPU: %v", err)
		}

		log.Info().Str("ID", id).Uint32("PID", pid).Msg("GPU freeze complete")

		// Begin GPU dump in parallel to CRIU dump

		go func() {
			defer close(dumpErr)
			waitCtx, cancel = context.WithTimeout(ctx, DUMP_TIMEOUT)
			defer cancel()

			_, err := controller.Dump(waitCtx, &gpu.DumpReq{Dir: opts.GetImagesDir(), Stream: stream > 0, LeaveRunning: opts.GetLeaveRunning()})
			if err != nil {
				log.Error().Err(err).Str("ID", id).Uint32("PID", pid).Msg("failed to dump GPU")
				dumpErr <- fmt.Errorf("failed to dump GPU: %v", err)
				return
			}
			log.Info().Str("ID", id).Uint32("PID", pid).Msg("GPU dump complete")
		}()
		return nil
	}

	// Wait for GPU dump to finish before finalizing the dump
	callback.PostDumpFunc = func(ctx context.Context, opts *criu_proto.CriuOpts) error {
		waitCtx, cancel := context.WithTimeout(ctx, UNFREEZE_TIMEOUT)
		defer cancel()

		pid := uint32(opts.GetPid())

		controller := m.controllers.Get(id)
		if controller == nil {
			return fmt.Errorf("GPU controller not found, is the process still running?")
		}

		_, err := controller.Unfreeze(waitCtx, &gpu.UnfreezeReq{})
		if err != nil {
			log.Error().Err(err).Str("ID", controller.ID).Uint32("PID", pid).Msg("failed to unfreeze GPU")
		}

		return errors.Join(err, <-dumpErr)
	}

	// Unfreeze on dump failure as well
	callback.OnDumpErrorFunc = func(ctx context.Context, opts *criu_proto.CriuOpts) {
		waitCtx, cancel := context.WithTimeout(ctx, UNFREEZE_TIMEOUT)
		defer cancel()

		pid := uint32(opts.GetPid())

		controller := m.controllers.Get(id)
		if controller == nil {
			log.Error().Uint32("PID", pid).Msg("GPU controller not found, is the process still running?")
			return
		}

		_, err := controller.Unfreeze(waitCtx, &gpu.UnfreezeReq{})
		if err != nil {
			log.Error().Err(err).Str("ID", controller.ID).Uint32("PID", pid).Msg("failed to unfreeze GPU on dump error")
		}

		return
	}

	// Add pre-restore hook for GPU restore, that begins GPU restore in parallel
	// to CRIU restore. We instead block at post-restore, to maximize concurrency.
	restoreErr := make(chan error, 1)
	callback.PreRestoreFunc = func(ctx context.Context, opts *criu_proto.CriuOpts) error {
		go func() {
			defer close(restoreErr)

			waitCtx, cancel := context.WithTimeout(ctx, RESTORE_TIMEOUT)
			defer cancel()

			pid := uint32(opts.GetPid())

			controller := m.controllers.Get(id)
			if controller == nil {
				restoreErr <- fmt.Errorf("GPU controller not found, is the process still running?")
				return
			}

			_, err := controller.Restore(waitCtx, &gpu.RestoreReq{Dir: opts.GetImagesDir(), Stream: stream > 0})
			if err != nil {
				log.Error().Err(err).Str("ID", controller.ID).Uint32("PID", pid).Msg("failed to restore GPU")
				restoreErr <- fmt.Errorf("failed to restore GPU: %v", err)
				return
			}
			log.Info().Str("ID", controller.ID).Uint32("PID", pid).Msg("GPU restore complete")

			// FIXME: It's not correct to add the below as components to the parent (PreRestoreFunc). Because
			// the restore happens inside a goroutine, the timing components belong to the restore goroutine (concurrent).

			// copyMemTime := time.Duration(resp.GetRestoreStats().GetCopyMemTime()) * time.Millisecond
			// replayCallsTime := time.Duration(resp.GetRestoreStats().GetReplayCallsTime()) * time.Millisecond
			// profiling.AddTimingComponent(ctx, copyMemTime, "controller.CopyMemory")
			// profiling.AddTimingComponent(ctx, replayCallsTime, "controller.ReplayCalls")
		}()
		return <-restoreErr // FIXME: This is until hostmem restore is fixed for runc
	}

	// Wait for GPU restore to finish before resuming the process
	callback.PostRestoreFunc = func(ctx context.Context, pid int32) error {
		return <-restoreErr
	}

	return callback
}

////////////////////////
//// Helper Methods ////
////////////////////////

func (i actionType) String() string {
	return [...]string{"maintainPool", "putController", "shutdown"}[i]
}

func (m *ManagerPool) syncWithDB(ctx context.Context, a action) error {
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

			err = m.controllers.Import(ctx, m.wg, dbController)
			if err != nil {
				// If import fails, we assume the controller is no longer running
				log.Debug().Str("reason", err.Error()).Str("ID", dbController.ID).Msg("clearing stale GPU controller in DB")
				m.pending <- action{putController, dbController.ID}
			}
		}

		freeList := m.controllers.FreeList()
		busyList := m.controllers.BusyList()
		activeList := slices.Concat(freeList, busyList)

		log.Debug().Int("free", len(freeList)).Int("busy", len(busyList)).Int("target", m.poolSize).Msg("GPU controller pool")

		// Remove controllers not in either free or busy list

		m.controllers.Range(func(key, value any) bool {
			c := value.(*controller)
			if !slices.ContainsFunc(activeList, func(c2 *controller) bool {
				return c2.ID == c.ID
			}) {
				log.Debug().Str("ID", c.ID).Msg("clearing stale GPU controller in pool")
				c.Terminate()
				m.controllers.Delete(c.ID)
				m.pending <- action{putController, c.ID}
			}
			return true
		})

		// Maintain the pool size

		if len(freeList) < m.poolSize {
			log.Debug().Int("target", m.poolSize).Int("current", len(freeList)).Msg("maintaining GPU pool size")
			for i := len(freeList); i < m.poolSize; i++ {
				controller, err := m.controllers.Spawn(m.plugins.Get("gpu").BinaryPaths()[0], nil)
				if err != nil {
					log.Debug().Err(err).Msg("failed to spawn GPU controller to maintain pool size")
					// Stop trying to spawn more right now as it could be an OOM condition
					return nil
				}
				log.Debug().Str("ID", controller.ID).Msg("spawned GPU controller to maintain pool size")
				m.pending <- action{putController, controller.ID}
			}
		} else if len(freeList) > m.poolSize {
			log.Debug().Int("target", m.poolSize).Int("current", len(freeList)).Msg("reducing GPU pool size")
			for i := len(freeList); i > m.poolSize; i-- {
				controller := freeList[i-1]
				log.Debug().Str("ID", controller.ID).Msg("terminating GPU controller to reduce pool size")
				controller.Terminate()
				m.controllers.Delete(controller.ID)
				m.pending <- action{putController, controller.ID}
			}
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
			})
		}
	}
	return err
}

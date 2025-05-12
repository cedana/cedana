package gpu

// Implements a simple GPU manager that always launches a GPU controller
// on demand on each attach request.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"syscall"
	"time"

	"buf.build/gen/go/cedana/cedana-gpu/protocolbuffers/go/gpu"
	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	criu_client "github.com/cedana/cedana/pkg/criu"
	"github.com/cedana/cedana/pkg/plugins"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/rs/zerolog/log"
)

const (
	FREEZE_TIMEOUT   = 20 * time.Second
	UNFREEZE_TIMEOUT = 20 * time.Second
	DUMP_TIMEOUT     = 5 * time.Minute
	RESTORE_TIMEOUT  = 5 * time.Minute
	HEALTH_TIMEOUT   = 30 * time.Second
)

type ManagerSimple struct {
	controllers controllers
	plugins     plugins.Manager
	wg          *sync.WaitGroup
}

func NewSimpleManager(serverWg *sync.WaitGroup, plugins plugins.Manager) *ManagerSimple {
	return &ManagerSimple{
		controllers: controllers{},
		plugins:     plugins,
		wg:          serverWg,
	}
}

func (m *ManagerSimple) Attach(ctx context.Context, lifetime context.Context, jid string, user *syscall.Credential, pid <-chan uint32, env []string) error {
	// Check if GPU plugin is installed
	var gpuPlugin *plugins.Plugin
	if gpuPlugin = m.plugins.Get("gpu"); !gpuPlugin.IsInstalled() {
		return fmt.Errorf("Please install the GPU plugin to use GPU support")
	}
	binary := gpuPlugin.BinaryPaths()[0]

	if _, err := os.Stat(binary); err != nil {
		return err
	}

	err := m.controllers.spawn(ctx, lifetime, m.wg, binary, jid, user, env, pid)
	if err != nil {
		return err
	}

	log.Debug().Str("JID", jid).Msg("GPU controller ready")

	return nil
}

func (m *ManagerSimple) AttachAsync(ctx context.Context, lifetime context.Context, jid string, user *syscall.Credential, pid <-chan uint32, env []string) <-chan error {
	err := make(chan error)

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		defer close(err)
		select {
		case <-ctx.Done():
			err <- ctx.Err()
		case err <- m.Attach(ctx, lifetime, jid, user, pid, env):
		}
	}()

	return err
}

func (m *ManagerSimple) Detach(jid string) error {
	return m.controllers.kill(jid)
}

func (m *ManagerSimple) IsAttached(jid string) bool {
	return m.controllers.Get(jid) != nil
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

		user, err := utils.GetCredentials()
		if err != nil {
			statusComponent.Data = "failed"
			statusComponent.Errors = append(statusComponent.Errors, fmt.Sprintf("Failed to get user credentials: %v", err))
			return []*daemon.HealthCheckComponent{statusComponent}
		}

		// Spawn a random controller and perform a health check
		jid := fmt.Sprintf("health-check-%d", time.Now().UnixNano())
		err = m.controllers.spawnAsync(ctx, m.wg, binary, jid, user, []string{})
		if err != nil {
			statusComponent.Data = "failed"
			statusComponent.Errors = append(statusComponent.Errors, fmt.Sprintf("Failed controller spawn: %v", err))
			return []*daemon.HealthCheckComponent{statusComponent}
		}

		controller := m.controllers.Get(jid)
		components, err := controller.waitForHealthCheck(ctx, m.wg)
		defer m.controllers.kill(jid)
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

func (m *ManagerSimple) CRIUCallback(lifetime context.Context, jid string, user *syscall.Credential, stream int32, env ...string) *criu_client.NotifyCallback {
	callback := &criu_client.NotifyCallback{Name: "gpu"}

	// Add pre-dump hook for GPU dump. We freeze the GPU controller so we can
	// do the GPU dump in parallel to CRIU dump.
	dumpErr := make(chan error, 1)
	callback.PreDumpFunc = func(ctx context.Context, opts *criu_proto.CriuOpts) error {
		waitCtx, cancel := context.WithTimeout(ctx, FREEZE_TIMEOUT)
		defer cancel()

		controller := m.controllers.Get(jid)
		if controller == nil {
			return fmt.Errorf("GPU controller not found, is the task still running?")
		}

		_, err := controller.Freeze(waitCtx, &gpu.FreezeReq{})
		if err != nil {
			log.Error().Err(err).Str("JID", jid).Msg("failed to freeze GPU")
			return fmt.Errorf("failed to freeze GPU: %v", err)
		}

		// Begin GPU dump in parallel to CRIU dump

		go func() {
			defer close(dumpErr)
			waitCtx, cancel = context.WithTimeout(ctx, DUMP_TIMEOUT)
			defer cancel()

			_, err := controller.Dump(waitCtx, &gpu.DumpReq{Dir: opts.GetImagesDir(), Stream: stream > 0, LeaveRunning: opts.GetLeaveRunning()})
			if err != nil {
				log.Error().Err(err).Str("JID", jid).Msg("failed to dump GPU")
				dumpErr <- fmt.Errorf("failed to dump GPU: %v", err)
				return
			}
			log.Info().Str("JID", jid).Msg("GPU dump complete")
		}()
		return nil
	}

	// Wait for GPU dump to finish before finalizing the dump
	callback.PostDumpFunc = func(ctx context.Context, opts *criu_proto.CriuOpts) error {
		waitCtx, cancel := context.WithTimeout(ctx, UNFREEZE_TIMEOUT)
		defer cancel()

		controller := m.controllers.Get(jid)
		if controller == nil {
			return fmt.Errorf("GPU controller not found, is the task still running?")
		}

		_, err := controller.Unfreeze(waitCtx, &gpu.UnfreezeReq{})
		if err != nil {
			log.Error().Err(err).Str("JID", jid).Msg("failed to unfreeze GPU")
		}

		return errors.Join(err, <-dumpErr)
	}

	// Unfreeze on dump failure as well
	callback.OnDumpErrorFunc = func(ctx context.Context) {
		waitCtx, cancel := context.WithTimeout(ctx, UNFREEZE_TIMEOUT)
		defer cancel()

		controller := m.controllers.Get(jid)
		if controller == nil {
			log.Error().Str("JID", jid).Msg("GPU controller not found, is the task still running?")
			return
		}

		_, err := controller.Unfreeze(waitCtx, &gpu.UnfreezeReq{})
		if err != nil {
			log.Error().Err(err).Str("JID", jid).Msg("failed to unfreeze GPU")
		}

		return
	}

	// Add pre-restore hook for GPU restore, that begins GPU restore in parallel
	// to CRIU restore. We instead block at post-restore, to maximize concurrency.
	restoreErr := make(chan error, 1)
	pidChan := make(chan uint32, 1)
	callback.InitializeRestoreFunc = func(ctx context.Context, opts *criu_proto.CriuOpts) error {
		err := m.Attach(ctx, lifetime, jid, user, pidChan, env) // Re-attach a GPU to the job
		if err != nil {
			return err
		}

		go func() {
			defer close(restoreErr)

			waitCtx, cancel := context.WithTimeout(ctx, RESTORE_TIMEOUT)
			defer cancel()

			controller := m.controllers.Get(jid)
			if controller == nil {
				restoreErr <- fmt.Errorf("GPU controller not found, is the task still running?")
			}

			_, err := controller.Restore(waitCtx, &gpu.RestoreReq{Dir: opts.GetImagesDir(), Stream: stream > 0})
			if err != nil {
				log.Error().Err(err).Str("JID", jid).Msg("failed to restore GPU")
				restoreErr <- fmt.Errorf("failed to restore GPU: %v", err)
				return
			}
			log.Info().Str("JID", jid).Msg("GPU restore complete")

			// FIXME: It's not correct to add the below as components to the parent (PreRestoreFunc). Because
			// the restore happens inside a goroutine, the timing components belong to the restore goroutine (concurrent).

			// copyMemTime := time.Duration(resp.GetRestoreStats().GetCopyMemTime()) * time.Millisecond
			// replayCallsTime := time.Duration(resp.GetRestoreStats().GetReplayCallsTime()) * time.Millisecond
			// profiling.AddTimingComponent(ctx, copyMemTime, "controller.CopyMemory")
			// profiling.AddTimingComponent(ctx, replayCallsTime, "controller.ReplayCalls")
		}()
		return nil
	}

	// Wait for GPU restore to finish before resuming the process
	callback.PostRestoreFunc = func(ctx context.Context, pid int32) error {
		pidChan <- uint32(pid)
		return <-restoreErr
	}

	// If CRIU fails to restore, detach the GPU controller
	callback.OnRestoreErrorFunc = func(ctx context.Context) {
		err := m.Detach(jid)
		if err != nil {
			log.Warn().Err(err).Str("JID", jid).Msg("failed to detach GPU controller on restore error")
		}
	}

	return callback
}

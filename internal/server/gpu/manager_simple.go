package gpu

// Implements a simple GPU manager that always launches a GPU controller
// on demand on each attach request.

import (
	"context"
	"fmt"
	"os"
	"sync"
	"syscall"
	"time"

	"buf.build/gen/go/cedana/cedana-gpu/protocolbuffers/go/gpu"
	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/internal/server/validation"
	"github.com/cedana/cedana/pkg/criu"
	"github.com/cedana/cedana/pkg/plugins"
	"github.com/rs/zerolog/log"
)

const (
	DUMP_TIMEOUT    = 5 * time.Minute
	RESTORE_TIMEOUT = 5 * time.Minute
	HEALTH_TIMEOUT  = 30 * time.Second
)

type ManagerSimple struct {
	controllers Controllers
	plugins     plugins.Manager
	wg          *sync.WaitGroup
}

func NewSimpleManager(serverWg *sync.WaitGroup, plugins plugins.Manager) *ManagerSimple {
	return &ManagerSimple{
		controllers: Controllers{},
		plugins:     plugins,
		wg:          serverWg,
	}
}

func (m *ManagerSimple) Attach(ctx context.Context, lifetime context.Context, jid string, pid <-chan uint32) error {
	// Check if GPU plugin is installed
	var gpuPlugin *plugins.Plugin
	if gpuPlugin = m.plugins.Get("gpu"); gpuPlugin.Status != plugins.Installed {
		return fmt.Errorf("Please install the GPU plugin to use GPU support")
	}
	binary := gpuPlugin.BinaryPaths()[0]

	if _, err := os.Stat(binary); err != nil {
		return err
	}

	err := m.controllers.Spawn(ctx, lifetime, m.wg, binary, jid, pid)
	if err != nil {
		return err
	}

	log.Debug().Str("JID", jid).Msg("GPU controller ready")

	return nil
}

func (m *ManagerSimple) AttachAsync(ctx context.Context, lifetime context.Context, jid string, pid <-chan uint32) <-chan error {
	err := make(chan error)

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		defer close(err)
		select {
		case <-ctx.Done():
			err <- ctx.Err()
		case err <- m.Attach(ctx, lifetime, jid, pid):
		}
	}()

	return err
}

func (m *ManagerSimple) Detach(ctx context.Context, jid string) error {
	controller := m.controllers.Get(jid)
	if controller != nil {
		m.controllers.Delete(jid)
		return controller.Process.Signal(syscall.SIGTERM)
	}
	return fmt.Errorf("No GPU attached to job %s", jid)
}

func (m *ManagerSimple) IsAttached(jid string) bool {
	return m.controllers.Get(jid) != nil
}

func (m *ManagerSimple) CRIUCallback(lifetime context.Context, jid string) *criu.NotifyCallback {
	callback := &criu.NotifyCallback{Name: "gpu"}

	// Add pre-dump hook for GPU dump. This ensures that the GPU is dumped before
	// CRIU freezes the process.
	callback.PreDumpFunc = func(ctx context.Context, opts *criu_proto.CriuOpts) error {
		err := validation.CheckCRIUOptsCompatibilityGPU(opts)
		if err != nil {
			return err
		}

		waitCtx, cancel := context.WithTimeout(ctx, DUMP_TIMEOUT)
		defer cancel()

		controller := m.controllers.Get(jid)

		_, err = controller.Checkpoint(waitCtx, &gpu.CheckpointRequest{Directory: opts.GetImagesDir()})
		if err != nil {
			log.Error().Err(err).Str("JID", jid).Msg("failed to dump GPU")
			return err
		}
		log.Info().Str("JID", jid).Msg("GPU dump complete")
		return err
	}

	// Add pre-restore hook for GPU restore, that begins GPU restore in parallel
	// to CRIU restore. We instead block at pre-resume, to maximize concurrency.
	restoreErr := make(chan error, 1)
	pidChan := make(chan uint32, 1)
	callback.PreRestoreFunc = func(ctx context.Context, opts *criu_proto.CriuOpts) error {
		err := validation.CheckCRIUOptsCompatibilityGPU(opts)
		if err != nil {
			return err
		}

		err = m.Attach(ctx, lifetime, jid, pidChan) // Re-attach a GPU to the job
		if err != nil {
			return err
		}

		go func() {
			defer close(restoreErr)

			waitCtx, cancel := context.WithTimeout(ctx, RESTORE_TIMEOUT)
			defer cancel()

			controller := m.controllers.Get(jid)
			_, err = controller.Restore(waitCtx, &gpu.RestoreRequest{Directory: opts.GetImagesDir()})
			if err != nil {
				log.Error().Err(err).Str("JID", jid).Msg("failed to restore GPU")
				restoreErr <- err
				return
			}
			log.Info().Str("JID", jid).Msg("GPU restore complete")
		}()
		return nil
	}

	// Wait for GPU restore to finish before resuming the process
	callback.PreResumeFunc = func(ctx context.Context, pid int32) error {
		pidChan <- uint32(pid)
		return <-restoreErr
	}

	return callback
}

package gpu

// Implements a simple GPU manager that always launches a GPU controller
// on demand on each attach request.

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"syscall"
	"time"

	"buf.build/gen/go/cedana/cedana-gpu/grpc/go/gpu/gpugrpc"
	"buf.build/gen/go/cedana/cedana-gpu/protocolbuffers/go/gpu"
	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/internal/server/validation"
	"github.com/cedana/cedana/pkg/criu"
	"github.com/cedana/cedana/pkg/plugins"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	CONTROLLER_HOST               = "localhost"
	CONTROLLER_LOG_PATH_FORMATTER = "/tmp/cedana-gpu-controller-%s.log"
	CONTROLLER_LOG_FLAGS          = os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	CONTROLLER_LOG_PERMS          = 0644

	HEALTH_TIMEOUT  = 30 * time.Second
	DUMP_TIMEOUT    = 5 * time.Minute
	RESTORE_TIMEOUT = 5 * time.Minute

	// Signal sent to job when GPU controller exits prematurely. The intercepted job
	// is guaranteed to exit upon receiving this signal, and prints to stderr
	// about the GPU controller's failure.
	CONTROLLER_PREMATURE_EXIT_SIGNAL = syscall.SIGUSR1
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
	var gpu *plugins.Plugin
	if gpu = m.plugins.Get("gpu"); gpu.Status != plugins.Installed {
		return fmt.Errorf("Please install the GPU plugin to use GPU support")
	}
	binary := gpu.BinaryPaths()[0]

	if _, err := os.Stat(binary); err != nil {
		return err
	}

	port, err := utils.GetFreePort()
	if err != nil {
		return err
	}

	cmd := exec.CommandContext(lifetime, binary, jid, "--port", strconv.Itoa(port))

	stdout, err := os.OpenFile(
		fmt.Sprintf(CONTROLLER_LOG_PATH_FORMATTER, jid),
		CONTROLLER_LOG_FLAGS,
		CONTROLLER_LOG_PERMS)
	if err != nil {
		return fmt.Errorf("failed to open GPU controller log file: %w", err)
	}

	stderr := &bytes.Buffer{}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Pdeathsig: syscall.SIGTERM,
	}
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) } // NO SIGKILL!!!

	err = cmd.Start()
	if err != nil {
		return fmt.Errorf(
			"failed to start GPU controller: %w",
			utils.GRPCErrorShort(err, stderr.String()),
		)
	}

	opts := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
	conn, err := grpc.NewClient(fmt.Sprintf("%s:%d", CONTROLLER_HOST, port), opts...)
	if err != nil {
		cmd.Process.Signal(syscall.SIGTERM)
		return fmt.Errorf(
			"failed to create GPU controller client: %w",
			utils.GRPCErrorShort(err, stderr.String()),
		)
	}
	client := gpugrpc.NewControllerClient(conn)

	controller := &Controller{
		JID:              jid,
		stderr:           stderr,
		ClientConn:       conn,
		ControllerClient: client,
		Cmd:              cmd,
	}
	m.controllers.Store(jid, controller)

	// Cleanup controller on exit, and signal job of its exit

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()

		err := cmd.Wait()
		if err != nil {
			log.Trace().Err(err).Str("JID", jid).Msg("GPU controller Wait()")
		}
		log.Debug().
			Int("code", cmd.ProcessState.ExitCode()).
			Str("JID", jid).
			Msg("GPU controller exited")

		conn.Close()
		m.controllers.Delete(jid)

		select {
		case <-lifetime.Done():
		case pid := <-pid:
			syscall.Kill(int(pid), CONTROLLER_PREMATURE_EXIT_SIGNAL)
		}
	}()

	log.Debug().Str("JID", jid).Int("port", port).Msg("waiting for GPU controller...")

	err = controller.WaitForHealthCheck(ctx, m.wg)
	if err != nil {
		cmd.Process.Signal(syscall.SIGTERM)
		conn.Close()
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

func (m *ManagerSimple) CRIUCallback(lifetime context.Context, jid string) criu.NotifyCallback {
	callback := criu.NotifyCallback{}

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

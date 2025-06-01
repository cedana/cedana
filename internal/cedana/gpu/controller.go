package gpu

// Internal definitions for the GPU controller

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"buf.build/gen/go/cedana/cedana-gpu/grpc/go/gpu/gpugrpc"
	"buf.build/gen/go/cedana/cedana-gpu/protocolbuffers/go/gpu"
	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	criu_proto "buf.build/gen/go/cedana/criu/protocolbuffers/go/criu"
	"github.com/cedana/cedana/internal/db"
	"github.com/cedana/cedana/pkg/config"
	criu_client "github.com/cedana/cedana/pkg/criu"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/shirou/gopsutil/v4/process"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	CONTROLLER_ADDRESS_FORMATTER         = "unix:///tmp/cedana-gpu-controller-%s.sock"
	CONTROLLER_SHM_FILE_FORMATTER        = "/dev/shm/cedana-gpu.%s"
	CONTROLLER_TERMINATE_SIGNAL          = syscall.SIGTERM
	GPU_CONTROLLER_PREMATURE_EXIT_SIGNAL = syscall.SIGUSR1 // Used to signal that the GPU controller exited prematurely

	FREEZE_TIMEOUT   = 1 * time.Minute
	UNFREEZE_TIMEOUT = 1 * time.Minute
	DUMP_TIMEOUT     = 5 * time.Minute
	RESTORE_TIMEOUT  = 5 * time.Minute
	HEALTH_TIMEOUT   = 30 * time.Second
)

type controller struct {
	ID            string
	AttachedPID   uint32
	ErrBuf        *bytes.Buffer
	PendingAttach atomic.Bool
	FreezeType    gpu.FreezeType

	*exec.Cmd
	gpugrpc.ControllerClient
	*grpc.ClientConn
}

type pool struct {
	sync.Map
}

// Get a specific GPU controller
func (p *pool) Get(id string) *controller {
	c, ok := p.Load(id)
	if !ok {
		return nil
	}
	return c.(*controller)
}

// Finds the GPU controller for the attached PID
func (p *pool) Find(attachedPID uint32) *controller {
	var found *controller
	p.Range(func(key, value any) bool {
		c := value.(*controller)
		if c.AttachedPID == attachedPID {
			found = c
			return false
		}
		return true
	})
	return found
}

// Imports an existing (DB) GPU controller
func (p *pool) Import(ctx context.Context, c *db.GPUController) error {
	controller := &controller{
		ID:          c.ID,
		AttachedPID: c.AttachedPID,
		FreezeType:  c.FreezeType,
		ErrBuf:      &bytes.Buffer{},
	}

	if !utils.PidRunning(c.PID) {
		return fmt.Errorf("no longer running")
	}

	process, err := os.FindProcess(int(c.PID))
	if err != nil {
		return err
	}
	controller.Cmd = &exec.Cmd{
		Process: process,
	}

	p.Store(c.ID, controller)

	return nil
}

// Returns a list of all GPU controllers grouped by free, pending, and busy states
func (p *pool) List() (free []*controller, pending []*controller, busy []*controller, remaining []*controller) {
	p.Range(func(key, value any) bool {
		c := value.(*controller)
		if c.PendingAttach.Load() {
			pending = append(pending, c)
			return true
		}
		if utils.PidRunning(uint32(c.Process.Pid)) {
			if c.AttachedPID == 0 {
				free = append(free, c)
				return true
			}
			if utils.PidRunning(c.AttachedPID) {
				busy = append(busy, c)
				return true
			}
		}
		remaining = append(remaining, c)
		return true
	})
	return
}

// Gets a free GPU controller
func (p *pool) GetFree() *controller {
	free, _, _, _ := p.List()
	if len(free) == 0 {
		return nil
	}
	for _, c := range free {
		if c.PendingAttach.CompareAndSwap(false, true) {
			return c
		}
	}
	return nil
}

// Spawns a GPU controller
func (p *pool) Spawn(binary string) (*controller, error) {
	// Generate a unique ID for the GPU controller
	id := uuid.NewString()

	observability := ""
	if config.Global.GPU.Observability {
		observability = "--observability"
	}

	controller := &controller{
		ID:     id,
		ErrBuf: &bytes.Buffer{},
		Cmd:    exec.Command(binary, id, observability, "--log-dir", config.Global.GPU.LogDir),
	}

	// We create a new process group and session to essentially daemonize the controller process.
	// So that workers of the controller can all be signaled together.

	controller.SysProcAttr = &syscall.SysProcAttr{
		Setsid:                     true,  // Create a new session and process group for the controller
		GidMappingsEnableSetgroups: false, // Avoid permission issues when running as non-root user
	}

	controller.Stderr = controller.ErrBuf

	controller.Env = append(
		os.Environ(),
		"CEDANA_URL="+config.Global.Connection.URL,
		"CEDANA_AUTH_TOKEN="+config.Global.Connection.AuthToken,
		"CEDANA_GPU_SHM_SIZE="+fmt.Sprintf("%d", config.Global.GPU.ShmSize),
	)

	err := controller.Start()
	if err != nil {
		return nil, fmt.Errorf(
			"failed to start GPU controller: %w",
			utils.GRPCErrorShort(err, controller.ErrBuf.String()),
		)
	}

	controller.PendingAttach.Store(true)

	p.Store(id, controller)

	return controller, nil
}

// Connect to an existing GPU controller.
func (c *controller) Connect(ctx context.Context) error {
	opts := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
	address := fmt.Sprintf(CONTROLLER_ADDRESS_FORMATTER, c.ID)
	conn, err := grpc.NewClient(address, opts...)
	if err != nil {
		return fmt.Errorf(
			"failed to create GPU controller client: %w",
			utils.GRPCErrorShort(err, c.ErrBuf.String()),
		)
	}
	c.ClientConn = conn
	c.ControllerClient = gpugrpc.NewControllerClient(conn)

	_, err = c.WaitForHealthCheck(ctx)
	if err != nil {
		return err
	}

	return nil
}

func (c *controller) Terminate() {
	defer func() {
		if c.AttachedPID == 0 {
			return
		}
		process, err := process.NewProcess(int32(c.AttachedPID))
		if err != nil {
			return
		}
		process.SendSignal(GPU_CONTROLLER_PREMATURE_EXIT_SIGNAL)
	}()

	if c.Process == nil {
		return
	}
	c.Process.Signal(CONTROLLER_TERMINATE_SIGNAL)
	if c.ClientConn != nil {
		c.ClientConn.Close()
	}
	process, err := process.NewProcess(int32(c.Process.Pid))
	if err != nil {
		log.Error().Err(err).Msg("failed to get process of GPU controller")
		return
	}
	parent, err := process.Parent()
	if err != nil {
		log.Error().Err(err).Msg("failed to get parent process of GPU controller")
		return
	}
	if os.Getpid() == int(parent.Pid) {
		// If the parent is the current process, we can wait for it to exit
		// This is useful for cleaning up the controller process when the daemon exits
		c.Process.Wait()
	}
}

// Health checks the GPU controller, blocking on connection until ready.
// This can be used as a proxy to wait for the controller to be ready.
func (c *controller) WaitForHealthCheck(ctx context.Context) ([]*daemon.HealthCheckComponent, error) {
	waitCtx, cancel := context.WithTimeout(ctx, HEALTH_TIMEOUT)
	defer cancel()

	// Wait for early controller exit, and cancel the blocking health check
	go func() {
		<-utils.WaitForPidCtx(waitCtx, uint32(c.Process.Pid))
		cancel()
	}()

	resp, err := c.HealthCheck(waitCtx, &gpu.HealthCheckReq{}, grpc.WaitForReady(true))
	var components []*daemon.HealthCheckComponent
	if resp != nil {
		l := log.Debug()
		l.Str("ID", c.ID)
		for _, component := range resp.Components {
			l = l.Str(component.Name, component.Data)
			for _, w := range component.Warnings {
				log.Warn().Str("ID", c.ID).Str(component.Name, component.Data).Msg(w)
			}
			for _, e := range component.Errors {
				log.Error().Str("ID", c.ID).Str(component.Name, component.Data).Msg(e)
			}
			components = append(components, &daemon.HealthCheckComponent{
				Name:     component.Name,
				Data:     component.Data,
				Warnings: component.Warnings,
				Errors:   component.Errors,
			})
		}
		l.Msg("health checked GPU controller")
	}
	if err != nil {
		return components, utils.GRPCErrorShort(err, c.ErrBuf.String())
	}

	return components, nil
}

func (p *pool) CRIUCallback(id string) *criu_client.NotifyCallback {
	callback := &criu_client.NotifyCallback{Name: "gpu"}

	// Add pre-dump hook for GPU dump. We freeze the GPU controller so we can
	// do the GPU dump in parallel to CRIU dump.
	dumpErr := make(chan error, 1)
	callback.PreDumpFunc = func(ctx context.Context, opts *criu_proto.CriuOpts) error {
		waitCtx, cancel := context.WithTimeout(ctx, FREEZE_TIMEOUT)
		defer cancel()

		pid := uint32(opts.GetPid())

		controller := p.Get(id)
		if controller == nil {
			return fmt.Errorf("GPU controller not found, is the process still running?")
		}

		// Make sure we are connected
		if controller.ClientConn == nil {
			err := controller.Connect(ctx)
			if err != nil {
				return fmt.Errorf("failed to connect to GPU controller: %v", err)
			}
		}

		_, err := controller.Freeze(waitCtx, &gpu.FreezeReq{Type: controller.FreezeType})
		if err != nil {
			log.Error().Err(err).Str("ID", id).Uint32("PID", pid).Msg("failed to freeze GPU")
			return fmt.Errorf("failed to freeze GPU: %v", utils.GRPCError(err))
		}

		log.Info().Str("ID", id).Uint32("PID", pid).Msg("GPU freeze complete")

		// Begin GPU dump in parallel to CRIU dump

		go func() {
			defer close(dumpErr)
			waitCtx, cancel = context.WithTimeout(ctx, DUMP_TIMEOUT)
			defer cancel()

			_, err := controller.Dump(waitCtx, &gpu.DumpReq{Dir: opts.GetImagesDir(), Stream: opts.GetStream(), LeaveRunning: opts.GetLeaveRunning()})
			if err != nil {
				log.Error().Err(err).Str("ID", id).Uint32("PID", pid).Msg("failed to dump GPU")
				dumpErr <- fmt.Errorf("failed to dump GPU: %v", utils.GRPCError(err))
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

		controller := p.Get(id)
		if controller == nil {
			return fmt.Errorf("GPU controller not found, is the process still running?")
		}

		_, err := controller.Unfreeze(waitCtx, &gpu.UnfreezeReq{})
		if err != nil {
			log.Error().Err(err).Str("ID", controller.ID).Uint32("PID", pid).Msg("failed to unfreeze GPU")
		}

		return errors.Join(err, utils.GRPCError(<-dumpErr))
	}

	// Unfreeze on dump failure as well
	callback.OnDumpErrorFunc = func(ctx context.Context, opts *criu_proto.CriuOpts) {
		waitCtx, cancel := context.WithTimeout(ctx, UNFREEZE_TIMEOUT)
		defer cancel()

		pid := uint32(opts.GetPid())

		controller := p.Get(id)
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

			controller := p.Get(id)
			if controller == nil {
				restoreErr <- fmt.Errorf("GPU controller not found, is the process still running?")
				return
			}

			_, err := controller.Restore(waitCtx, &gpu.RestoreReq{Dir: opts.GetImagesDir(), Stream: opts.GetStream()})
			if err != nil {
				log.Error().Err(err).Str("ID", controller.ID).Uint32("PID", pid).Msg("failed to restore GPU")
				restoreErr <- fmt.Errorf("failed to restore GPU: %v", utils.GRPCError(err))
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
		return nil
	}

	// Wait for GPU restore to finish before resuming the process
	callback.PostRestoreFunc = func(ctx context.Context, pid int32) error {
		return <-restoreErr
	}

	return callback
}

func (p *pool) Check(binary string) types.Check {
	return func(ctx context.Context) []*daemon.HealthCheckComponent {
		component := &daemon.HealthCheckComponent{Name: "status"}

		// Spawn a new GPU controller

		controller, err := p.Spawn(binary)
		defer func() {
			controller.Terminate()
			p.Delete(controller.ID)
		}()
		if err != nil {
			component.Data = "failed"
			component.Errors = append(component.Errors, fmt.Sprintf("Failed controller spawn: %v", err))
			return []*daemon.HealthCheckComponent{component}
		}

		err = controller.Connect(ctx)
		if err != nil {
			component.Data = "failed"
			component.Errors = append(component.Errors, fmt.Sprintf("Failed controller connect: %v", err))
			return []*daemon.HealthCheckComponent{component}
		}

		components, err := controller.WaitForHealthCheck(ctx)
		if components == nil && err != nil {
			component.Data = "failed"
			component.Errors = append(component.Errors, fmt.Sprintf("Failed controller health check: %v", err))
			return []*daemon.HealthCheckComponent{component}
		}

		component.Data = "available"

		return append([]*daemon.HealthCheckComponent{component}, components...)
	}
}

package gpu

// Internal definitions for the GPU controller

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"

	"buf.build/gen/go/cedana/cedana-gpu/grpc/go/gpu/gpugrpc"
	"buf.build/gen/go/cedana/cedana-gpu/protocolbuffers/go/gpu"
	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/internal/db"
	"github.com/cedana/cedana/pkg/config"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/shirou/gopsutil/v4/process"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	CONTROLLER_ADDRESS_FORMATTER  = "unix:///tmp/cedana-gpu-controller-%s.sock"
	CONTROLLER_SHM_FILE_FORMATTER = "/dev/shm/cedana-gpu.%s"
	CONTROLLER_TERMINATE_SIGNAL   = syscall.SIGTERM
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

type controllers struct {
	sync.Map
}

// Get a specific GPU controller
func (m *controllers) Get(id string) *controller {
	c, ok := m.Load(id)
	if !ok {
		return nil
	}
	return c.(*controller)
}

// Finds the GPU controller for the attached PID
func (m *controllers) Find(attachedPID uint32) *controller {
	var found *controller
	m.Range(func(key, value any) bool {
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
func (m *controllers) Import(ctx context.Context, wg *sync.WaitGroup, c *db.GPUController) error {
	controller := &controller{
		ID:          c.ID,
		AttachedPID: c.AttachedPID,
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

	m.Store(c.ID, controller)

	return nil
}

// Returns a list of all GPU controllers grouped by free, pending, and busy states
func (m *controllers) List() (free []*controller, pending []*controller, busy []*controller, remaining []*controller) {
	m.Range(func(key, value any) bool {
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
func (m *controllers) GetFree() *controller {
	free, _, _, _ := m.List()
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
func (m *controllers) Spawn(binary string) (*controller, error) {
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

	controller.Stderr = controller.ErrBuf
	controller.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true, // So it can run independently in its own process group
	}

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

	m.Store(id, controller)

	return controller, nil
}

// Connect to an existing GPU controller.
func (controller *controller) Connect(ctx context.Context, wg *sync.WaitGroup) error {
	opts := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
	address := fmt.Sprintf(CONTROLLER_ADDRESS_FORMATTER, controller.ID)
	conn, err := grpc.NewClient(address, opts...)
	if err != nil {
		return fmt.Errorf(
			"failed to create GPU controller client: %w",
			utils.GRPCErrorShort(err, controller.ErrBuf.String()),
		)
	}
	controller.ClientConn = conn
	controller.ControllerClient = gpugrpc.NewControllerClient(conn)

	_, err = controller.WaitForHealthCheck(ctx, wg)
	if err != nil {
		return err
	}

	return nil
}

func (controller *controller) Terminate() {
	if controller.Process == nil {
		return
	}
	controller.Process.Signal(CONTROLLER_TERMINATE_SIGNAL)
	if controller.ClientConn != nil {
		controller.ClientConn.Close()
	}
	process, err := process.NewProcess(int32(controller.Process.Pid))
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
		controller.Process.Wait()
		// Clean up controller resources, if not already done
		os.Remove(strings.TrimPrefix(fmt.Sprintf(CONTROLLER_ADDRESS_FORMATTER, controller.ID), "unix://"))
		os.Remove(fmt.Sprintf(CONTROLLER_SHM_FILE_FORMATTER, controller.ID))
	}
}

// Health checks the GPU controller, blocking on connection until ready.
// This can be used as a proxy to wait for the controller to be ready.
func (controller *controller) WaitForHealthCheck(ctx context.Context, wg *sync.WaitGroup) ([]*daemon.HealthCheckComponent, error) {
	waitCtx, cancel := context.WithTimeout(ctx, HEALTH_TIMEOUT)
	defer cancel()

	// Wait for early controller exit, and cancel the blocking health check
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-utils.WaitForPidCtx(waitCtx, uint32(controller.Process.Pid))
		cancel()
	}()

	resp, err := controller.HealthCheck(waitCtx, &gpu.HealthCheckReq{}, grpc.WaitForReady(true))
	var components []*daemon.HealthCheckComponent
	if resp != nil {
		l := log.Debug()
		l.Str("ID", controller.ID)
		for _, c := range resp.Components {
			l = l.Str(c.Name, c.Data)
			for _, w := range c.Warnings {
				log.Warn().Str("ID", controller.ID).Str(c.Name, c.Data).Msg(w)
			}
			for _, e := range c.Errors {
				log.Error().Str("ID", controller.ID).Str(c.Name, c.Data).Msg(e)
			}
			components = append(components, &daemon.HealthCheckComponent{
				Name:     c.Name,
				Data:     c.Data,
				Warnings: c.Warnings,
				Errors:   c.Errors,
			})
		}
		l.Msg("health checked GPU controller")
	}
	if err != nil {
		return components, utils.GRPCErrorShort(err, controller.ErrBuf.String())
	}

	return components, nil
}

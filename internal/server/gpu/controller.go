package gpu

// Internal definitions for the GPU controller

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"

	"buf.build/gen/go/cedana/cedana-gpu/grpc/go/gpu/gpugrpc"
	"buf.build/gen/go/cedana/cedana-gpu/protocolbuffers/go/gpu"
	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/internal/db"
	"github.com/cedana/cedana/pkg/config"
	"github.com/cedana/cedana/pkg/logging"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	CONTROLLER_ADDRESS_FORMATTER  = "unix:///tmp/cedana-gpu-controller-%s.sock"
	CONTROLLER_TERMINATE_SIGNAL   = syscall.SIGTERM
	CONTROLLER_LOG_FILE_FORMATTER = "cedana-gpu-controller-%s.log"
	CONTROLLER_LOG_FILE_MODE      = os.O_CREATE | os.O_WRONLY | os.O_APPEND
	CONTROLLER_LOG_FILE_PERMS     = 0o644
)

type controller struct {
	ID          string
	AttachedPID uint32
	ErrBuf      *bytes.Buffer

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
		if uint32(c.Process.Pid) == attachedPID {
			found = c
			return false
		}
		return true
	})
	return found
}

// Imports an existing (DB) GPU controller, and connects to it
func (m *controllers) Import(ctx context.Context, wg *sync.WaitGroup, c *db.GPUController) error {
	controller := &controller{
		ID:          c.ID,
		AttachedPID: c.AttachedPID,
		ErrBuf:      &bytes.Buffer{},
	}

	process, err := os.FindProcess(int(c.PID))
	if err != nil {
		return err
	}
	controller.Cmd = &exec.Cmd{
		Process: process,
	}

	err = controller.Connect(ctx, wg)
	if err != nil {
		return err
	}

	m.Store(c.ID, controller)

	return nil
}

// Spawns a GPU controller
func (m *controllers) Spawn(
	binary string,
	user *syscall.Credential,
	env ...string,
) (*controller, error) {
	// Generate a unique ID for the GPU controller
	id := uuid.NewString()

	observability := ""
	if config.Global.GPU.Observability {
		observability = "--observability"
	}

	controller := &controller{
		ErrBuf: &bytes.Buffer{},
		Cmd:    exec.Command(binary, id, observability),
	}

	controller.Stderr = controller.ErrBuf

	if dir := config.Global.GPU.LogDir; dir != "" {
		file, err := os.OpenFile(
			filepath.Join(dir, fmt.Sprintf(CONTROLLER_LOG_FILE_FORMATTER, id)),
			CONTROLLER_LOG_FILE_MODE,
			CONTROLLER_LOG_FILE_PERMS,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to open log file for GPU controller: %w", err)
		}
		controller.Stdout = file
	} else {
		controller.Stdout = logging.Writer("gpu-controller", id, zerolog.TraceLevel)
	}
	controller.SysProcAttr = &syscall.SysProcAttr{
		Credential: user,
	}
	controller.Env = append(
		os.Environ(),
		"CEDANA_URL="+config.Global.Connection.URL,
		"CEDANA_AUTH_TOKEN="+config.Global.Connection.AuthToken,
	)

	// Add user, runtime-specific environment variables.
	// Could potentially override os.Environ() variables, which is intended.
	controller.Env = append(controller.Env, env...)

	err := controller.Start()
	if err != nil {
		return nil, fmt.Errorf(
			"failed to start GPU controller: %w",
			utils.GRPCErrorShort(err, controller.ErrBuf.String()),
		)
	}

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
		controller.Terminate()
		return err
	}

	return nil
}

func (controller *controller) Terminate() {
	controller.Process.Signal(CONTROLLER_TERMINATE_SIGNAL)
	controller.Close()
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
		for _, c := range resp.Components {
			l = l.Str(c.Name, c.Data)
			for _, w := range c.Warnings {
				log.Warn().Str(c.Name, c.Data).Msg(w)
			}
			for _, e := range c.Errors {
				log.Error().Str(c.Name, c.Data).Msg(e)
			}
			components = append(components, &daemon.HealthCheckComponent{
				Name:     c.Name,
				Data:     c.Data,
				Warnings: c.Warnings,
				Errors:   c.Errors,
			})
		}
		l.Msg("GPU health check")
	}
	if err != nil {
		return components, utils.GRPCErrorShort(err, controller.ErrBuf.String())
	}

	return components, nil
}

package gpu

// Internal definitions for the GPU controller

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"sync"
	"syscall"

	"buf.build/gen/go/cedana/cedana-gpu/grpc/go/gpu/gpugrpc"
	"buf.build/gen/go/cedana/cedana-gpu/protocolbuffers/go/gpu"
	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/logging"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	CONTROLLER_HOST = "localhost"

	// Signal sent to job when GPU controller exits prematurely. The intercepted job
	// is guaranteed to exit upon receiving this signal, and prints to stderr
	// about the GPU controller's failure.
	CONTROLLER_PREMATURE_EXIT_SIGNAL = syscall.SIGUSR1

	MIN_SHM_SIZE = 8 << 30 // 8 GiB
)

type controller struct {
	ErrBuf *bytes.Buffer

	*exec.Cmd
	gpugrpc.ControllerClient
	*grpc.ClientConn
}

type controllers struct {
	sync.Map
}

func (m *controllers) get(jid string) *controller {
	c, ok := m.Load(jid)
	if !ok {
		return nil
	}
	return c.(*controller)
}

// Spawns a GPU controller and blocks until it is ready. Performs
// a blocking health check call to the controller to ensure it is ready.
// Takes an optional PID chan, to tell the which process is it being attached to.
// If the controller dies prematurely, a special signal is sent to the process.
func (m *controllers) spawn(
	ctx context.Context,
	lifetime context.Context,
	wg *sync.WaitGroup,
	binary string,
	jid string,
	pid ...<-chan uint32,
) error {
	err := m.spawnAsync(ctx, lifetime, wg, binary, jid, pid...)
	if err != nil {
		return err
	}

	controller := m.get(jid)

	log.Debug().Str("jid", jid).Msg("waiting for GPU controller...")

	_, err = controller.waitForHealthCheck(ctx, wg)
	if err != nil {
		controller.Process.Signal(syscall.SIGTERM)
		controller.Close()
		return err
	}

	return nil
}

// Spawns a GPU controller in the background
func (m *controllers) spawnAsync(
	ctx context.Context,
	lifetime context.Context,
	wg *sync.WaitGroup,
	binary string,
	jid string,
	pid ...<-chan uint32,
) error {
	port, err := utils.GetFreePort()
	if err != nil {
		return fmt.Errorf("failed to get free port: %w", err)
	}

	controller := &controller{
		ErrBuf: &bytes.Buffer{},
		Cmd:    exec.CommandContext(lifetime, binary, jid, "--port", strconv.Itoa(port)),
	}

	controller.Stderr = controller.ErrBuf
	controller.Stdin = nil
	controller.Stdout = logging.Writer("gpu-controller", jid, zerolog.TraceLevel)
	controller.SysProcAttr = &syscall.SysProcAttr{
		Pdeathsig: syscall.SIGTERM,
	}
	controller.Cancel = func() error { return controller.Cmd.Process.Signal(syscall.SIGTERM) } // NO SIGKILL!!!

	err = controller.Start()
	if err != nil {
		return fmt.Errorf(
			"failed to start GPU controller: %w",
			utils.GRPCErrorShort(err, controller.ErrBuf.String()),
		)
	}

	opts := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
	conn, err := grpc.NewClient(fmt.Sprintf("%s:%d", CONTROLLER_HOST, port), opts...)
	if err != nil {
		controller.Process.Signal(syscall.SIGTERM)
		return fmt.Errorf(
			"failed to create GPU controller client: %w",
			utils.GRPCErrorShort(err, controller.ErrBuf.String()),
		)
	}
	controller.ClientConn = conn
	controller.ControllerClient = gpugrpc.NewControllerClient(conn)

	m.Store(jid, controller)

	// Cleanup controller on exit, and signal job of its exit

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer conn.Close()

		err := controller.Wait()
		if err != nil {
			log.Trace().Err(err).Msg("GPU controller Wait()")
		}
		log.Debug().Int("code", controller.ProcessState.ExitCode()).Msg("GPU controller exited")

		m.Delete(jid)

		if len(pid) > 0 {
			select {
			case <-lifetime.Done():
			case pid := <-pid[0]:
				syscall.Kill(int(pid), CONTROLLER_PREMATURE_EXIT_SIGNAL)
			}
		}
	}()

	return nil
}

func (m *controllers) kill(jid string) error {
	controller := m.get(jid)
	if controller == nil {
		return fmt.Errorf("No GPU controller attached for %s", jid)
	}
	controller.Process.Signal(syscall.SIGTERM)
	return nil
}

// Health checks the GPU controller, blocking on connection until ready.
// This can be used as a proxy to wait for the controller to be ready.
func (controller *controller) waitForHealthCheck(ctx context.Context, wg *sync.WaitGroup) ([]*daemon.HealthCheckComponent, error) {
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
		controller.Process.Signal(syscall.SIGTERM)
		controller.Close()
		return components, utils.GRPCErrorShort(err, controller.ErrBuf.String())
	}
	return components, nil
}

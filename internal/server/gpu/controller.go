package gpu

// Internal definitions for the GPU controller

import (
	"bytes"
	"context"
	"os/exec"
	"sync"
	"syscall"

	"buf.build/gen/go/cedana/cedana-gpu/grpc/go/gpu/gpugrpc"
	"buf.build/gen/go/cedana/cedana-gpu/protocolbuffers/go/gpu"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Controller struct {
	JID    string
	stderr *bytes.Buffer

	*exec.Cmd
	gpugrpc.ControllerClient
	*grpc.ClientConn
}

type Controllers struct {
	sync.Map
}

func (m *Controllers) Get(jid string) *Controller {
	c, ok := m.Load(jid)
	if !ok {
		return nil
	}
	return c.(*Controller)
}

// Health checks the GPU controller, blocking on connection until ready.
// This can be used as a proxy to wait for the controller to be ready.
func (controller *Controller) WaitForHealthCheck(ctx context.Context, wg *sync.WaitGroup) error {
	waitCtx, cancel := context.WithTimeout(ctx, HEALTH_TIMEOUT)
	defer cancel()

	// Wait for early controller exit, and cancel the blocking health check
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-utils.WaitForPidCtx(waitCtx, uint32(controller.Process.Pid))
		cancel()
	}()

	resp, err := controller.HealthCheck(waitCtx, &gpu.HealthCheckRequest{}, grpc.WaitForReady(true))
	if resp != nil {
		log.Debug().
			Str("JID", controller.JID).
			Int32("devices", resp.DeviceCount).
			Str("version", resp.Version).
			Int32("driver", resp.GetAvailableAPIs().GetDriverVersion()).
			Msg("GPU health check")
	}
	if err != nil || !resp.Success {
		controller.Process.Signal(syscall.SIGTERM)
		controller.Close()
		if err == nil {
			err = status.Errorf(codes.FailedPrecondition, "GPU health check failed")
			controller.stderr.WriteString("GPU health check failed")
		}
		return utils.GRPCErrorShort(err, controller.stderr.String())
	}
	return nil
}

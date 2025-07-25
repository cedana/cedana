package cedana

import (
	"context"
	"time"

	"buf.build/gen/go/cedana/cedana/grpc/go/daemon/daemongrpc"
	cedana_io "github.com/cedana/cedana/pkg/io"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const ATTACH_TIMEOUT = 1 * time.Minute

func (s *Server) Attach(stream daemongrpc.Daemon_AttachServer) error {
	in, err := stream.Recv()
	if err != nil {
		return err
	}
	pid := in.GetPID()
	if pid == 0 {
		return status.Errorf(codes.InvalidArgument, "missing PID")
	}

	// Check if the given process has an available IO slave
	slave := cedana_io.GetIOSlave(pid)
	if slave == nil {
		return status.Errorf(codes.NotFound, "process %d has no IO slave", pid)
	}

	ctx, cancel := context.WithTimeout(s.lifetime, ATTACH_TIMEOUT)
	defer cancel()

	err = slave.Attach(ctx, stream)
	if err != nil {
		if err == context.DeadlineExceeded {
			return status.Errorf(codes.DeadlineExceeded, "likely another master IO attached")
		}
		return err
	}
	log.Debug().Uint32("PID", pid).Msgf("master IO detached from process")

	return nil
}

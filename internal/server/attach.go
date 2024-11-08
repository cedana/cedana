package server

import (
	"context"
	"time"

	"github.com/cedana/cedana/pkg/api/daemon"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const ATTACH_TIMEOUT = 10 * time.Second

func (s *Server) Attach(stream daemon.Daemon_AttachServer) error {
	in, err := stream.Recv()
	if err != nil {
		return err
	}
	pid := in.GetPID()
	if pid == 0 {
		return status.Errorf(codes.InvalidArgument, "missing PID")
	}

	// Check if the given process has an available IO slave
	slave := utils.GetIOSlave(pid)
	if slave == nil {
		return status.Errorf(codes.NotFound, "process %d has no IO slave", pid)
	}

	lifetime, cancel := context.WithTimeout(s.ctx, ATTACH_TIMEOUT)
	defer cancel()

	err = slave.Attach(lifetime, stream)
	if err != nil {
		return err
	}
	log.Info().Uint32("PID", pid).Msgf("master detached from process")

	return nil
}

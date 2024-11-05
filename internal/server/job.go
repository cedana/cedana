package server

import (
	"context"
	"syscall"

	"github.com/cedana/cedana/pkg/api/daemon"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *Server) List(ctx context.Context, req *daemon.ListReq) (*daemon.ListResp, error) {
	jobs, err := s.db.ListJobs(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list jobs: %v", err)
	}

	return &daemon.ListResp{Jobs: jobs}, nil
}

func (s *Server) Kill(ctx context.Context, req *daemon.KillReq) (*daemon.KillResp, error) {
	jobs, err := s.db.ListJobs(ctx, req.GetJIDs()...)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list jobs: %v", err)
	}

	for _, job := range jobs {
		if job.GetProcess().GetInfo().GetIsRunning() {
			pid := job.GetProcess().GetPID()
			err = syscall.Kill(int(pid), syscall.SIGKILL)
			if err != nil {
				log.Error().Err(err).Msgf("failed to kill job %s", job.GetJID())
			}
		}
	}

	return &daemon.KillResp{}, err
}

func (s *Server) Delete(ctx context.Context, req *daemon.DeleteReq) (*daemon.DeleteResp, error) {
	jobs, err := s.db.ListJobs(ctx, req.GetJIDs()...)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list jobs: %v", err)
	}

	for _, job := range jobs {
		// Don't delete running jobs
		if job.GetProcess().GetInfo().GetIsRunning() {
			err = status.Errorf(codes.FailedPrecondition, "job %s is running", job.GetJID())
			continue
		}
		err = s.db.DeleteJob(ctx, job.GetJID())
		if err != nil {
			log.Error().Err(err).Msgf("failed to delete job %s", job.GetJID())
		}
	}

	return &daemon.DeleteResp{}, err
}

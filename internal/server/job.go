package server

import (
	"context"
	"errors"
	"fmt"
	"syscall"

	"github.com/cedana/cedana/pkg/api/daemon"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *Server) Attach(stream daemon.Daemon_AttachServer) error {
	in, err := stream.Recv()
	if err != nil {
		return err
	}
	jid := in.GetJID()

	// Check if the given job has an available IO slave
	slave := utils.GetIOSlave(jid)
	if slave == nil {
		return status.Errorf(codes.NotFound, "job %s has no IO slave", jid)
	}

	err = slave.Attach(s.ctx, stream)
	if err != nil {
		return err
	}
	log.Info().Str("JID", jid).Msgf("master detached from job")

	return nil
}

func (s *Server) List(ctx context.Context, req *daemon.ListReq) (*daemon.ListResp, error) {
	jobs, err := s.db.ListJobs(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list jobs: %v", err)
	}

	for _, job := range jobs {
		s.syncJobState(ctx, job)
	}

	return &daemon.ListResp{Jobs: jobs}, nil
}

func (s *Server) Kill(ctx context.Context, req *daemon.KillReq) (*daemon.KillResp, error) {
	jobs, err := s.db.ListJobs(ctx, req.GetJIDs()...)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list jobs: %v", err)
	}

	if len(jobs) == 0 {
		return nil, status.Errorf(codes.NotFound, "no jobs found")
	}

	errs := []error{}

	for _, job := range jobs {
		s.syncJobState(ctx, job)
		if job.GetProcess().GetInfo().GetIsRunning() {
			pid := job.GetProcess().GetPID()
			err = syscall.Kill(int(pid), syscall.SIGKILL)
			if err != nil {
				log.Error().Err(err).Msgf("failed to kill job %s", job.GetJID())
			}
			errs = append(errs, err)
		}
	}

	return &daemon.KillResp{}, errors.Join(errs...)
}

func (s *Server) Delete(ctx context.Context, req *daemon.DeleteReq) (*daemon.DeleteResp, error) {
	jobs, err := s.db.ListJobs(ctx, req.GetJIDs()...)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list jobs: %v", err)
	}

	if len(jobs) == 0 {
		return nil, status.Errorf(codes.NotFound, "no jobs found")
	}

	errs := []error{}

	for _, job := range jobs {
		s.syncJobState(ctx, job)
		// Don't delete running jobs
		if job.GetProcess().GetInfo().GetIsRunning() {
			errs = append(errs, fmt.Errorf("job %s is running", job.GetJID()))
			continue
		}
		err = s.db.DeleteJob(ctx, job.GetJID())
		if err != nil {
			log.Error().Err(err).Msgf("failed to delete job %s", job.GetJID())
		}
		errs = append(errs, err)
	}

	return &daemon.DeleteResp{}, errors.Join(errs...)
}

///////////////////
///// Helpers /////
///////////////////

// syncJobState checks if the job is still running and updates the state
func (s *Server) syncJobState(ctx context.Context, job *daemon.Job) {
	if job.GetProcess().GetInfo().GetIsRunning() {
		pid := job.GetProcess().GetPID()
		exists := utils.PidExists(pid)
		if !exists {
			job.GetProcess().GetInfo().IsRunning = false
			s.db.PutJob(ctx, job.JID, job)
		}
	}
}

package server

import (
	"context"
	"errors"
	"fmt"
	"syscall"

	"buf.build/gen/go/cedana/daemon/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const LOG_ATTACHABLE string = "[Attachable]"

func (s *Server) List(ctx context.Context, req *daemon.ListReq) (*daemon.ListResp, error) {
	jobs, err := s.db.ListJobs(ctx, req.GetJIDs()...)
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
	isRunning := job.GetProcess().GetInfo().GetIsRunning()
	pid := job.GetProcess().GetPID()

	if isRunning {
		if !utils.PidExists(pid) {
			job.GetProcess().GetInfo().IsRunning = false
		}
	}
	s.db.PutJob(ctx, job.JID, job)

	// Ephemeral changes, that we don't want to save to DB
	if isRunning && utils.GetIOSlave(pid) != nil {
		job.Log = LOG_ATTACHABLE
	}
}

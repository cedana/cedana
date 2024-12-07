package server

import (
	"context"
	"errors"
	"fmt"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	cedana_io "github.com/cedana/cedana/pkg/io"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const LOG_ATTACHABLE string = "[Attachable]"

func (s *Server) List(ctx context.Context, req *daemon.ListReq) (*daemon.ListResp, error) {
	jobs := s.jobs.List(req.GetJIDs()...)

	jobProtos := []*daemon.Job{}
	for _, job := range jobs {
		proto := *job.GetProto()
		if job.IsRunning() && cedana_io.GetIOSlave(job.GetPID()) != nil {
			proto.Log = LOG_ATTACHABLE
		}

		jobProtos = append(jobProtos, &proto)
	}

	return &daemon.ListResp{Jobs: jobProtos}, nil
}

func (s *Server) Kill(ctx context.Context, req *daemon.KillReq) (*daemon.KillResp, error) {
	jobs := s.jobs.List(req.GetJIDs()...)

	if len(jobs) == 0 {
		return nil, status.Errorf(codes.NotFound, "no jobs found")
	}

	errs := []error{}

	for _, job := range jobs {
		if job.IsRunning() {
			err := s.jobs.Kill(job.JID)
			if err != nil {
				log.Error().Err(err).Msgf("failed to kill job %s", job.JID)
			}
			errs = append(errs, err)
		}
	}

	return &daemon.KillResp{}, errors.Join(errs...)
}

func (s *Server) Delete(ctx context.Context, req *daemon.DeleteReq) (*daemon.DeleteResp, error) {
	jobs := s.jobs.List(req.GetJIDs()...)

	if len(jobs) == 0 {
		return nil, status.Errorf(codes.NotFound, "no jobs found")
	}

	errs := []error{}

	for _, job := range jobs {
		// Don't delete running jobs
		if job.IsRunning() {
			errs = append(errs, fmt.Errorf("job %s is running", job.JID))
			continue
		}
		s.jobs.Delete(job.JID)
	}

	return &daemon.DeleteResp{}, errors.Join(errs...)
}

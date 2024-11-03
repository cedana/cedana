package server

import (
	"context"

	"github.com/cedana/cedana/pkg/api/daemon"
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

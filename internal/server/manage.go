package server

import (
	"context"

	"github.com/cedana/cedana/pkg/api/daemon"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *Server) Manage(ctx context.Context, req *daemon.ManageReq) (*daemon.ManageResp, error) {
	return nil, status.Error(codes.Unimplemented, "method Manage not implemented")
}

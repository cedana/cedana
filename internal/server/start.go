package server

import (
	"context"

	"github.com/cedana/cedana/pkg/api/daemon"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *Server) Start(ctx context.Context, req *daemon.StartReq) (*daemon.StartResp, error) {
	return nil, status.Error(codes.Unimplemented, "method Exec not implemented")
}

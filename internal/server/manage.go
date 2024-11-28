package server

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/plugins"
	"github.com/cedana/cedana/pkg/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Pluggable features
const featureManageHandler plugins.Feature[types.Manage] = "ManageHandler"

func (s *Server) Manage(ctx context.Context, req *daemon.ManageReq) (*daemon.ManageResp, error) {
	return nil, status.Error(codes.Unimplemented, "method Manage not implemented")
}

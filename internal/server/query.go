package server

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/features"
	"github.com/cedana/cedana/pkg/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *Server) Query(ctx context.Context, req *daemon.QueryReq) (*daemon.QueryResp, error) {
	var handler types.Query

	// Get handler for query based on type
	err := features.QueryHandler.IfAvailable(func(name string, pluginHander types.Query) error {
		handler = pluginHander
		return nil
	}, req.Type)
	if err != nil {
		return nil, status.Errorf(codes.Unimplemented, "query handler for type %s not found: %v", req.Type, err)
	}

	return handler(ctx, req)
}

package cedana

import (
	"context"
	"fmt"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/internal/cedana/process"
	"github.com/cedana/cedana/pkg/features"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/pkg/utils"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *Server) Query(ctx context.Context, req *daemon.QueryReq) (*daemon.QueryResp, error) {
	var handler types.Query

	// Get handler for query based on type

	switch req.Type {
	case "process":
		handler = process.Query
	default:
		err := features.QueryHandler.IfAvailable(func(name string, pluginHander types.Query) error {
			handler = pluginHander
			return nil
		}, req.Type)
		if err != nil {
			return nil, status.Errorf(codes.Unimplemented, "query handler for `%s` not found: %v", req.Type, err)
		}
	}

	resp, err := handler(ctx, req)
	if err != nil {
		return nil, err
	}

	// Enrich the process state data

	for i, state := range resp.States {
		err = utils.FillProcessState(ctx, state.PID, state, req.Tree)
		if err != nil {
			resp.States = append(resp.States[:i], resp.States[i+1:]...)
			resp.Messages = append(resp.Messages, fmt.Sprintf("PID %d: %v", state.PID, err))
		}
	}

	return resp, nil
}

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

	// Drop the states that can't be filled (e.g. a container whose task never started has PID 0),
	// compacting in place: deleting while ranging would skip entries and run past the shortened slice
	filled := resp.States[:0]
	for _, state := range resp.States {
		if err := utils.FillProcessState(ctx, state.PID, state, req.Tree); err != nil {
			resp.Messages = append(resp.Messages, fmt.Sprintf("PID %d: %v", state.PID, err))
			continue
		}
		filled = append(filled, state)
	}
	resp.States = filled

	return resp, nil
}

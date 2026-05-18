package process

import (
	"context"
	"fmt"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Defines the process query handler

func Query(ctx context.Context, req *daemon.QueryReq) (*daemon.QueryResp, error) {
	query := req.PIDs
	if query == nil {
		return nil, status.Errorf(codes.InvalidArgument, "no PIDs provided for process query")
	}

	// Simply set the PID in state for later enrichment

	resp := &daemon.QueryResp{}

	for _, pid := range query {
		state := &daemon.ProcessState{
			PID: pid,
		}
		resp.States = append(resp.States, state)
	}

	resp.Messages = append(resp.Messages, fmt.Sprintf("Found %d process(es)", len(resp.States)))

	return resp, nil
}

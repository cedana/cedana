package validation

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Adapter that validates the run request
func ValidateRunRequest(next types.Run) types.Run {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RunResp, req *daemon.RunReq) (code func() <-chan int, err error) {
		if req.GetType() == "" {
			return nil, status.Errorf(codes.InvalidArgument, "Type is required")
		}
		if req.GetDetails() == nil {
			return nil, status.Errorf(codes.InvalidArgument, "Details are required")
		}
		// Check if JID already exists
		return next(ctx, opts, resp, req)
	}
}

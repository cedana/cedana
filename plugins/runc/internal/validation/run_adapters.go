package validation

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func ValidateRunRequest(next types.Run) types.Run {
	return func(ctx context.Context, server types.ServerOpts, resp *daemon.RunResp, req *daemon.RunReq) (chan int, error) {
		if req.GetDetails().GetRunc() == nil {
			return nil, status.Errorf(codes.InvalidArgument, "missing runc run options")
		}
		if req.GetDetails().GetRunc().GetRoot() == "" {
			return nil, status.Errorf(codes.InvalidArgument, "missing root")
		}
		if req.GetDetails().GetRunc().GetID() == "" {
			return nil, status.Errorf(codes.InvalidArgument, "missing id")
		}
		if req.GetDetails().GetRunc().GetBundle() == "" {
			return nil, status.Errorf(codes.InvalidArgument, "missing bundle")
		}

		return next(ctx, server, resp, req)
	}
}

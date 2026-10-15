package validation

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func ValidateRunRequest(next types.Run) types.Run {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RunResp, req *daemon.RunReq) (code func() <-chan int, err error) {
		details := req.GetDetails().GetPodman()

		if details == nil {
			return nil, status.Errorf(codes.InvalidArgument, "missing podman details")
		}
		if details.ID == "" {
			return nil, status.Errorf(codes.InvalidArgument, "missing podman container")
		}
		if req.Action == daemon.RunAction_START_NEW && details.Image == "" {
			return nil, status.Errorf(codes.InvalidArgument, "missing podman image")
		}

		return next(ctx, opts, resp, req)
	}
}

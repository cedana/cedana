package validation

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func ValidateRestoreRequest(next types.Restore) types.Restore {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (code func() <-chan int, err error) {
		details := req.GetDetails().GetPodman()

		if details == nil {
			return nil, status.Errorf(codes.InvalidArgument, "missing podman details")
		}
		if details.ID == "" {
			return nil, status.Errorf(codes.InvalidArgument, "missing podman container name")
		}
		if details.Image == "" {
			return nil, status.Errorf(codes.InvalidArgument, "missing podman image")
		}

		return next(ctx, opts, resp, req)
	}
}

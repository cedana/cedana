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
		if req.GetDetails().GetSlurm() == nil {
			return nil, status.Errorf(codes.InvalidArgument, "missing slurm run options")
		}
		if req.GetDetails().GetSlurm().GetJobID() == 0 {
			return nil, status.Errorf(codes.InvalidArgument, "missing slurm job id")
		}

		return next(ctx, opts, resp, req)
	}
}

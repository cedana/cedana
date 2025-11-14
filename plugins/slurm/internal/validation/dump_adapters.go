package validation

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func ValidateDumpRequest(next types.Dump) types.Dump {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (func() <-chan int, error) {
		if req.GetDetails().GetSlurm() == nil {
			return nil, status.Errorf(codes.InvalidArgument, "missing slurm details")
		}
		if req.GetDetails().GetSlurm().GetID() == "" {
			return nil, status.Errorf(codes.InvalidArgument, "missing slurm id")
		}

		return next(ctx, opts, resp, req)
	}
}

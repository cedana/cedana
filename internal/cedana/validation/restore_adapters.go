package validation

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Adapter that validates the restore request
func ValidateRestoreRequest(next types.Restore) types.Restore {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (code func() <-chan int, err error) {
		if req.GetPath() == "" {
			return nil, status.Error(codes.InvalidArgument, "no path provided")
		}
		if req.GetType() == "" {
			return nil, status.Error(codes.InvalidArgument, "missing type")
		}

		return next(ctx, opts, resp, req)
	}
}

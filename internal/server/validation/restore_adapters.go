package validation

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/internal/server/streamer"
	"github.com/cedana/cedana/pkg/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Adapter that validates the restore request
func ValidateRestoreRequest(next types.Restore) types.Restore {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RestoreResp, req *daemon.RestoreReq) (exited chan int, err error) {
		if req.GetPath() == "" {
			return nil, status.Error(codes.InvalidArgument, "no path provided")
		}
		if req.GetType() == "" {
			return nil, status.Error(codes.InvalidArgument, "missing type")
		}
		if req.GetStream() < 0 || req.GetStream() > streamer.MAX_PARALLELISM {
			return nil, status.Errorf(codes.InvalidArgument, "stream parallelism must be between 0 and %d", streamer.MAX_PARALLELISM)
		}

		return next(ctx, opts, resp, req)
	}
}

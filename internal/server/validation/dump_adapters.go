package validation

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/internal/server/streamer"
	"github.com/cedana/cedana/pkg/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Adapter that just checks all required fields are present in the request
func ValidateDumpRequest(next types.Dump) types.Dump {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (exited chan int, err error) {
		if req.GetDir() == "" {
			return nil, status.Errorf(codes.InvalidArgument, "no dump dir specified")
		}
		if req.GetDetails() == nil {
			return nil, status.Errorf(codes.InvalidArgument, "missing details")
		}
		if req.GetType() == "" {
			return nil, status.Errorf(codes.InvalidArgument, "missing type")
		}
		if req.GetStream() < 0 || req.GetStream() > streamer.MAX_PARALLELISM {
			return nil, status.Errorf(codes.InvalidArgument, "stream parallelism must be between 0 and %d", streamer.MAX_PARALLELISM)
		}

		return next(ctx, opts, resp, req)
	}
}

package validation

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Adapter that just checks all required fields are present in the request
func ValidateDumpVMRequest(next types.DumpVM) types.DumpVM {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpVMResp, req *daemon.DumpVMReq) (code func() <-chan int, err error) {
		if req.GetDetails() == nil {
			return nil, status.Errorf(codes.InvalidArgument, "missing details")
		}
		if req.GetType() == "" {
			return nil, status.Errorf(codes.InvalidArgument, "missing type")
		}

		return next(ctx, opts, resp, req)
	}
}

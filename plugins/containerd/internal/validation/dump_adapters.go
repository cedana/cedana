package validation

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func ValidateDumpRequest(next types.Dump) types.Dump {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (code func() <-chan int, err error) {
		details := req.GetDetails().GetContainerd()

		if details == nil {
			return nil, status.Errorf(codes.InvalidArgument, "missing containerd details")
		}
		if details.Address == "" {
			return nil, status.Errorf(codes.InvalidArgument, "missing containerd address")
		}
		if details.Namespace == "" {
			return nil, status.Errorf(codes.InvalidArgument, "missing containerd namespace")
		}
		if details.ID == "" {
			return nil, status.Errorf(codes.InvalidArgument, "missing containerd id")
		}
		if details.RootfsOnly && details.Rootfs && details.GetImage().GetName() == "" {
			return nil, status.Errorf(codes.InvalidArgument, "missing image ref for rootfs dump")
		}

		return next(ctx, opts, resp, req)
	}
}

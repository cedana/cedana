package validation

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func ValidateDumpRequst(next types.Dump) types.Dump {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (code func() <-chan int, err error) {
		if req.GetDetails().GetContainerd() == nil {
			return nil, status.Errorf(codes.InvalidArgument, "missing containerd details")
		}
		if req.GetDetails().GetContainerd().GetAddress() == "" {
			return nil, status.Errorf(codes.InvalidArgument, "missing containerd address")
		}
		if req.GetDetails().GetContainerd().GetNamespace() == "" {
			return nil, status.Errorf(codes.InvalidArgument, "missing containerd namespace")
		}
		if req.GetDetails().GetContainerd().GetID() == "" {
			return nil, status.Errorf(codes.InvalidArgument, "missing containerd id")
		}
		if req.GetDetails().GetContainerd().GetRootfsOnly() && req.GetDetails().GetContainerd().GetImage().GetName() == "" {
			return nil, status.Errorf(codes.InvalidArgument, "missing image ref for rootfs-only dump")
		}

		return next(ctx, opts, resp, req)
	}
}

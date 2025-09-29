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
		if req.Action == daemon.RunAction_START_NEW && req.GetDetails().GetContainerd().GetImage().GetName() == "" {
			return nil, status.Errorf(codes.InvalidArgument, "missing containerd image")
		}

		return next(ctx, opts, resp, req)
	}
}

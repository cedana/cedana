package adapters

// This file contains all the adapters that validate the request

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/criu"
	"github.com/cedana/cedana/pkg/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

//////////////////////
//// Run Adapters ////
//////////////////////

func ValidateRunRequest(next types.Run) types.Run {
	return func(ctx context.Context, server types.ServerOpts, resp *daemon.RunResp, req *daemon.RunReq) (chan int, error) {
		if req.GetDetails().GetRunc() == nil {
			return nil, status.Errorf(codes.InvalidArgument, "missing runc run options")
		}
		if req.GetDetails().GetRunc().GetRoot() == "" {
			return nil, status.Errorf(codes.InvalidArgument, "missing root")
		}
		if req.GetDetails().GetRunc().GetID() == "" {
			return nil, status.Errorf(codes.InvalidArgument, "missing id")
		}
		if req.GetDetails().GetRunc().GetBundle() == "" {
			return nil, status.Errorf(codes.InvalidArgument, "missing bundle")
		}

		return next(ctx, server, resp, req)
	}
}

///////////////////////
//// Dump Adapters ////
///////////////////////

func ValidateDumpRequest(next types.Dump) types.Dump {
	return func(ctx context.Context, server types.ServerOpts, nfy *criu.NotifyCallbackMulti, resp *daemon.DumpResp, req *daemon.DumpReq) (chan int, error) {
		if req.GetDetails().GetRunc() == nil {
			return nil, status.Errorf(codes.InvalidArgument, "missing runc run options")
		}
		if req.GetDetails().GetRunc().GetRoot() == "" {
			return nil, status.Errorf(codes.InvalidArgument, "missing root")
		}
		if req.GetDetails().GetRunc().GetID() == "" {
			return nil, status.Errorf(codes.InvalidArgument, "missing id")
		}

		return next(ctx, server, nfy, resp, req)
	}
}

//////////////////////////
//// Restore Adapters ////
//////////////////////////

func ValidateRestoreRequest(next types.Restore) types.Restore {
	return func(ctx context.Context, server types.ServerOpts, nfy *criu.NotifyCallbackMulti, resp *daemon.RestoreResp, req *daemon.RestoreReq) (chan int, error) {
		if req.GetDetails().GetRunc() == nil {
			return nil, status.Errorf(codes.InvalidArgument, "missing runc run options")
		}
		if req.GetDetails().GetRunc().GetRoot() == "" {
			return nil, status.Errorf(codes.InvalidArgument, "missing root")
		}
		if req.GetDetails().GetRunc().GetID() == "" {
			return nil, status.Errorf(codes.InvalidArgument, "missing id")
		}
		if req.GetDetails().GetRunc().GetBundle() == "" {
			return nil, status.Errorf(codes.InvalidArgument, "missing bundle")
		}

		return next(ctx, server, nfy, resp, req)
	}
}

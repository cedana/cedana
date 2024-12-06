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

// Adapter that validates the run request
func ValidateRunRequest(next types.Run) types.Run {
	return func(ctx context.Context, server types.ServerOpts, resp *daemon.RunResp, req *daemon.RunReq) (chan int, error) {
		if req.GetType() == "" {
			return nil, status.Errorf(codes.InvalidArgument, "Type is required")
		}
		if req.GetDetails() == nil {
			return nil, status.Errorf(codes.InvalidArgument, "Details are required")
		}
		// Check if JID already exists
		return next(ctx, server, resp, req)
	}
}

///////////////////////
//// Dump Adapters ////
///////////////////////

// Adapter that just checks all required fields are present in the request
func ValidateDumpRequest(next types.Dump) types.Dump {
	return func(ctx context.Context, server types.ServerOpts, nfy *criu.NotifyCallbackMulti, resp *daemon.DumpResp, req *daemon.DumpReq) (exited chan int, err error) {
		if req.GetDir() == "" {
			return nil, status.Errorf(codes.InvalidArgument, "no dump dir specified")
		}
		if req.GetDetails() == nil {
			return nil, status.Errorf(codes.InvalidArgument, "missing details")
		}
		if req.GetType() == "" {
			return nil, status.Errorf(codes.InvalidArgument, "missing type")
		}

		return next(ctx, server, nfy, resp, req)
	}
}

//////////////////////////
//// Restore Adapters ////
//////////////////////////

// Adapter that validates the restore request
func ValidateRestoreRequest(next types.Restore) types.Restore {
	return func(ctx context.Context, server types.ServerOpts, nfy *criu.NotifyCallbackMulti, resp *daemon.RestoreResp, req *daemon.RestoreReq) (chan int, error) {
		if req.GetPath() == "" {
			return nil, status.Error(codes.InvalidArgument, "no path provided")
		}
		if req.GetType() == "" {
			return nil, status.Error(codes.InvalidArgument, "missing type")
		}

		return next(ctx, server, nfy, resp, req)
	}
}

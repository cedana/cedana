package adapters

import (
	"context"
	"sync"

	"github.com/cedana/cedana/pkg/api/daemon"
	"github.com/cedana/cedana/pkg/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// This file contains all the adapters that validate the request

///////////////////////
//// Dump Adapters ////
///////////////////////

// Adapter that just checks all required fields are present in the request
func ValidateDumpRequest(next types.DumpHandler) types.DumpHandler {
	return func(ctx context.Context, wg *sync.WaitGroup, resp *daemon.DumpResp, req *daemon.DumpReq) error {
		if req.GetDir() == "" {
			return status.Errorf(codes.InvalidArgument, "no dump dir specified")
		}
		if req.GetDetails() == nil {
			return status.Errorf(codes.InvalidArgument, "missing details")
		}
		if req.GetType() == "" {
			return status.Errorf(codes.InvalidArgument, "missing type")
		}

		return next(ctx, wg, resp, req)
	}
}

//////////////////////////
//// Restore Adapters ////
//////////////////////////

// Adapter that validates the restore request
func ValidateRestoreRequest(next types.RestoreHandler) types.RestoreHandler {
	return func(ctx context.Context, lifetimeCtx context.Context, wg *sync.WaitGroup, resp *daemon.RestoreResp, req *daemon.RestoreReq) (chan int, error) {
		if req.GetPath() == "" {
			return nil, status.Error(codes.InvalidArgument, "no path provided")
		}
		if req.GetType() == "" {
			return nil, status.Error(codes.InvalidArgument, "missing type")
		}

		return next(ctx, lifetimeCtx, wg, resp, req)
	}
}

////////////////////////
//// Start Adapters ////
////////////////////////

// Adapter that validates the start request
func ValidateStartRequest(next types.StartHandler) types.StartHandler {
	return func(ctx context.Context, lifetimeCtx context.Context, wg *sync.WaitGroup, resp *daemon.StartResp, req *daemon.StartReq) (chan int, error) {
		if req.GetType() == "" {
			return nil, status.Errorf(codes.InvalidArgument, "Type is required")
		}
		if req.GetDetails() == nil {
			return nil, status.Errorf(codes.InvalidArgument, "Details are required")
		}
		// Check if JID already exists
		return next(ctx, lifetimeCtx, wg, resp, req)
	}
}

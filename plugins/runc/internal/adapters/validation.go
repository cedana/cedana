package adapters

import (
	"context"

	"buf.build/gen/go/cedana/daemon/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// This file contains all the adapters that validate the request

///////////////////////
//// Dump Adapters ////
///////////////////////

func ValidateDumpRequest(next types.Dump) types.Dump {
	return func(ctx context.Context, server types.ServerOpts, resp *daemon.DumpResp, req *daemon.DumpReq) (chan int, error) {
		if req.GetDetails().GetRunc().GetRoot() == "" {
			return nil, status.Errorf(codes.InvalidArgument, "missing root")
		}
		if req.GetDetails().GetRunc().GetID() == "" {
			return nil, status.Errorf(codes.InvalidArgument, "missing id")
		}

		return next(ctx, server, resp, req)
	}
}

//////////////////////////
//// Restore Adapters ////
//////////////////////////

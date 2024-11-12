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

func ValidateDumpRequest(h types.DumpHandler) types.DumpHandler {
	return func(ctx context.Context, wg *sync.WaitGroup, resp *daemon.DumpResp, req *daemon.DumpReq) error {
		if req.GetDetails().GetRunc().GetRoot() == "" {
			return status.Errorf(codes.InvalidArgument, "missing root")
		}
		if req.GetDetails().GetRunc().GetID() == "" {
			return status.Errorf(codes.InvalidArgument, "missing id")
		}

		return h(ctx, wg, resp, req)
	}
}

//////////////////////////
//// Restore Adapters ////
//////////////////////////

package adapters

// This file contains all the adapters that manage the container info

import (
	"context"

	"github.com/cedana/cedana/pkg/api/daemon"
	"github.com/cedana/cedana/pkg/types"
	runc_types "github.com/cedana/cedana/plugins/runc/pkg/types"
	"github.com/opencontainers/runc/libcontainer"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Defines all the adapters that use the container state

///////////////////////
//// Dump Adapters ////
///////////////////////

func GetContainerForDump(next types.Dump) types.Dump {
	return func(ctx context.Context, server types.ServerOpts, resp *daemon.DumpResp, req *daemon.DumpReq) error {
		root := req.GetDetails().GetRunc().GetRoot()
		id := req.GetDetails().GetRunc().GetID()

		container, err := libcontainer.Load(root, id)
		if err != nil {
			return status.Errorf(codes.Internal, "failed to load container: %v", err)
		}

		ctx = context.WithValue(ctx, runc_types.DUMP_CONTAINER_CONTEXT_KEY, container)

		return next(ctx, server, resp, req)
	}
}

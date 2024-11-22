package adapters

// This file contains all the adapters that manage container info

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/types"
	runc_keys "github.com/cedana/cedana/plugins/runc/pkg/keys"
	"github.com/opencontainers/runc/libcontainer"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

///////////////////////
//// Dump Adapters ////
///////////////////////

func GetContainerForDump(next types.Dump) types.Dump {
	return func(ctx context.Context, server types.ServerOpts, resp *daemon.DumpResp, req *daemon.DumpReq) (chan int, error) {
		root := req.GetDetails().GetRunc().GetRoot()
		id := req.GetDetails().GetRunc().GetID()

		container, err := libcontainer.Load(root, id)
		if err != nil {
			return nil, status.Errorf(codes.NotFound, "failed to load container: %v", err)
		}

		ctx = context.WithValue(ctx, runc_keys.DUMP_CONTAINER_CONTEXT_KEY, container)

		return next(ctx, server, resp, req)
	}
}

//////////////////////////
//// Restore Adapters ////
//////////////////////////

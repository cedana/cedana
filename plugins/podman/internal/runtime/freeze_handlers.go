package runtime

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/features"
	"github.com/cedana/cedana/pkg/types"
	podman_keys "github.com/cedana/cedana/plugins/podman/pkg/keys"
	"github.com/cedana/cedana/plugins/podman/pkg/utils"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var Freeze types.Freeze = freeze

func freeze(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (code func() <-chan int, err error) {
	container, ok := ctx.Value(podman_keys.CONTAINER_CONTEXT_KEY).(*utils.Container)
	if !ok {
		return nil, status.Errorf(codes.Internal, "failed to get container from context")
	}

	plugin, err := setRuntimeDetails(req, container)
	if err != nil {
		return nil, err
	}

	var runtimeFreezeHandler types.Freeze

	err = features.FreezeHandler.IfAvailable(func(_ string, runtimeHandler types.Freeze) error {
		runtimeFreezeHandler = runtimeHandler
		return nil
	}, plugin)
	if err != nil {
		return nil, status.Errorf(codes.FailedPrecondition, "unsupported runtime %s: %v", plugin, err)
	}

	return runtimeFreezeHandler(ctx, opts, resp, req)
}

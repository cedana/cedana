package runtime

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/plugins/runc"
	"github.com/cedana/cedana/pkg/features"
	"github.com/cedana/cedana/pkg/types"
	containerd_keys "github.com/cedana/cedana/plugins/containerd/pkg/keys"
	"github.com/cedana/cedana/plugins/containerd/pkg/utils"
	"github.com/containerd/containerd"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var Unfreeze types.Unfreeze = unfreeze

func unfreeze(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (code func() <-chan int, err error) {
	details := req.GetDetails()
	id := details.GetContainerd().GetID()
	namespace := details.GetContainerd().GetNamespace()

	client, ok := ctx.Value(containerd_keys.CLIENT_CONTEXT_KEY).(*containerd.Client)
	if !ok {
		return nil, status.Errorf(codes.Internal, "failed to get containerd client from context")
	}

	plugin := utils.PluginForRuntime(client.Runtime())

	var runtimeUnfreezeHandler types.Unfreeze

	err = features.UnfreezeHandler.IfAvailable(func(_ string, runtimeHandler types.Unfreeze) error {
		runtimeUnfreezeHandler = runtimeHandler
		return nil
	}, plugin)
	if err != nil {
		return nil, status.Errorf(codes.FailedPrecondition, "unsupported runtime %s: %v", client.Runtime(), err)
	}

	// Add runtime-specific details to the request

	switch plugin {
	case "runc":
		details.Runc = &runc.Runc{
			ID:   id,
			Root: utils.RootFromPlugin(plugin, namespace),
		}
	default:
		return nil, status.Errorf(codes.Unimplemented, "unsupported plugin %s", plugin)
	}

	return runtimeUnfreezeHandler(ctx, opts, resp, req)
}

package client

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/types"
	containerd_keys "github.com/cedana/cedana/plugins/containerd/pkg/keys"
	"github.com/containerd/containerd"
	"github.com/containerd/containerd/namespaces"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func SetupForRun(next types.Run) types.Run {
	return func(ctx context.Context, server types.ServerOpts, resp *daemon.RunResp, req *daemon.RunReq) (exited chan int, err error) {
		details := req.GetDetails().GetContainerd()

		ctx = namespaces.WithNamespace(ctx, details.Namespace)

		client, err := containerd.New(details.Address, containerd.WithDefaultNamespace(details.Namespace))
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to create containerd client: %v", err)
		}
		defer client.Close()

		ctx = context.WithValue(ctx, containerd_keys.CLIENT_CONTEXT_KEY, client)

		return next(ctx, server, resp, req)
	}
}

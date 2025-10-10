package client

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/types"
	containerd_keys "github.com/cedana/cedana/plugins/containerd/pkg/keys"
	"github.com/containerd/containerd"
	"github.com/containerd/containerd/namespaces"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func SetupForDump(next types.Dump) types.Dump {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (code func() <-chan int, err error) {
		details := req.GetDetails().GetContainerd()

		ctx = namespaces.WithNamespace(ctx, details.Namespace)

		client, err := containerd.New(details.Address)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to create containerd client: %v", err)
		}
		defer client.Close()

		ctx = context.WithValue(ctx, containerd_keys.CLIENT_CONTEXT_KEY, client)

		return next(ctx, opts, resp, req)
	}
}

func LoadContainerForDump(next types.Dump) types.Dump {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (code func() <-chan int, err error) {
		details := req.GetDetails().GetContainerd()

		client, ok := ctx.Value(containerd_keys.CLIENT_CONTEXT_KEY).(*containerd.Client)
		if !ok {
			return nil, status.Errorf(codes.Internal, "failed to get client from context")
		}

		container, err := client.LoadContainer(ctx, details.ID)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to load container %s: %v", details.ID, err)
		}

		log.Debug().Str("id", container.ID()).Msg("loaded container for dump")

		ctx = context.WithValue(ctx, containerd_keys.CONTAINER_CONTEXT_KEY, container)

		return next(ctx, opts, resp, req)
	}
}

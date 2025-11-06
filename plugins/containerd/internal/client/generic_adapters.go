package client

import (
	"context"

	"github.com/cedana/cedana/pkg/types"
	containerd_keys "github.com/cedana/cedana/plugins/containerd/pkg/keys"
	"github.com/containerd/containerd"
	"github.com/containerd/containerd/namespaces"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func Setup[REQ, RESP any](next types.Handler[REQ, RESP]) types.Handler[REQ, RESP] {
	return func(ctx context.Context, opts types.Opts, resp *RESP, req *REQ) (func() <-chan int, error) {
		details := types.Details(req).GetContainerd()

		ctx = namespaces.WithNamespace(ctx, details.Namespace)

		client, err := containerd.New(details.Address, containerd.WithDefaultNamespace(details.Namespace))
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to create containerd client: %v", err)
		}

		ctx = context.WithValue(ctx, containerd_keys.CLIENT_CONTEXT_KEY, client)

		return next(ctx, opts, resp, req)
	}
}

func LoadContainer[REQ, RESP any](next types.Handler[REQ, RESP]) types.Handler[REQ, RESP] {
	return func(ctx context.Context, opts types.Opts, resp *RESP, req *REQ) (func() <-chan int, error) {
		details := types.Details(req).GetContainerd()

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

func SetAdditionalEnv[REQ, RESP any](next types.Handler[REQ, RESP]) types.Handler[REQ, RESP] {
	return func(ctx context.Context, opts types.Opts, resp *RESP, req *REQ) (func() <-chan int, error) {
		env := types.Details(req).GetContainerd().GetEnv()
		if len(env) == 0 {
			return next(ctx, opts, resp, req)
		}

		container, ok := ctx.Value(containerd_keys.CONTAINER_CONTEXT_KEY).(containerd.Container)
		if !ok {
			return nil, status.Errorf(codes.Internal, "failed to get container from context")
		}
		spec, err := container.Spec(ctx)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to get spec from container: %v", err)
		}

		spec.Process.Env = append(spec.Process.Env, env...)

		err = container.Update(ctx, containerd.UpdateContainerOpts(containerd.WithSpec(spec)))
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to update container with new spec: %v", err)
		}

		return next(ctx, opts, resp, req)
	}
}

package client

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/types"
	containerd_keys "github.com/cedana/cedana/plugins/containerd/pkg/keys"
	"github.com/containerd/containerd"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/containerd/oci"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/opencontainers/runtime-spec/specs-go"
)

func SetupForRun(next types.Run) types.Run {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RunResp, req *daemon.RunReq) (exited chan int, err error) {
		details := req.GetDetails().GetContainerd()

		ctx = namespaces.WithNamespace(ctx, details.Namespace)

		client, err := containerd.New(details.Address, containerd.WithDefaultNamespace(details.Namespace))
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to create containerd client: %v", err)
		}
		defer client.Close()

		ctx = context.WithValue(ctx, containerd_keys.CLIENT_CONTEXT_KEY, client)

		return next(ctx, opts, resp, req)
	}
}

func CreateContainerForRun(next types.Run) types.Run {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RunResp, req *daemon.RunReq) (exited chan int, err error) {
		details := req.GetDetails().GetContainerd()

		client, ok := ctx.Value(containerd_keys.CLIENT_CONTEXT_KEY).(*containerd.Client)
		if !ok {
			return nil, status.Errorf(codes.Internal, "failed to get client from context")
		}

		var container containerd.Container

		switch req.Action {
		case daemon.RunAction_START_NEW:

			image, err := client.GetImage(ctx, details.Image)
			if err != nil {
				return nil, status.Errorf(codes.Internal, "failed to get image: %v", err)
			}

			var opts []oci.SpecOpts
			opts = append(opts, oci.WithDefaultSpec(), oci.WithDefaultUnixDevices)
			opts = append(opts, oci.WithImageConfig(image))
			var spec specs.Spec
			var cOpts []containerd.NewContainerOpts
			cOpts = append(cOpts, containerd.WithImage(image))
			cOpts = append(cOpts, containerd.WithSpec(&spec, opts...))
			cOpts = append(cOpts, containerd.WithNewSnapshot(details.ID, image))

			defer func() {
				if err != nil {
					container.Delete(ctx)
				}
			}()
			ctx = context.WithValue(ctx, containerd_keys.SPEC_CONTEXT_KEY, &spec)
			ctx = context.WithValue(ctx, containerd_keys.SPEC_CONTEXT_KEY, &spec)
			ctx = context.WithValue(ctx, containerd_keys.CONTAINER_OPTS_CONTEXT_KEY, cOpts)

		case daemon.RunAction_MANAGE_EXISTING:
			container, err = client.LoadContainer(ctx, details.ID)
			if err != nil {
				return nil, status.Errorf(codes.Internal, "failed to load container: %v", err)
			}
			ctx = context.WithValue(ctx, containerd_keys.MANAGE_CONTAINER_CONTEXT_KEY, container)
		}

		return next(ctx, opts, resp, req)
	}
}

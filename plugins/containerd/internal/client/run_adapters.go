package client

import (
	"context"
	"time"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/types"
	containerd_keys "github.com/cedana/cedana/plugins/containerd/pkg/keys"
	"github.com/containerd/containerd"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/containerd/oci"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const waitForManageUpcomingTimeout = 2 * time.Minute

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

			container, err = client.NewContainer(
				ctx,
				details.ID,
				containerd.WithImage(image),
				containerd.WithNewSnapshot(details.ID+"-snapshot", image),
				containerd.WithNewSpec(oci.WithImageConfig(image)),
			)
			if err != nil {
				return nil, status.Errorf(codes.Internal, "failed to create container for run: %v", err)
			}
			defer func() {
				if err != nil {
					container.Delete(ctx)
				}
			}()

		case daemon.RunAction_MANAGE_EXISTING:

			container, err = client.LoadContainer(ctx, details.ID)
			if err != nil {
				return nil, status.Errorf(codes.Internal, "failed to load container: %v", err)
			}

		case daemon.RunAction_MANAGE_UPCOMING:
		loop:
			for {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-opts.Lifetime.Done():
					return nil, opts.Lifetime.Err()
				case <-time.After(500 * time.Millisecond):
					container, err = client.LoadContainer(ctx, details.ID)
					if err == nil {
						break loop
					}
					log.Trace().Str("id", details.ID).Msg("waiting for upcoming container to start managing")
				case <-time.After(waitForManageUpcomingTimeout):
					return nil, status.Errorf(codes.DeadlineExceeded, "timed out waiting for upcoming container %s", details.ID)
				}
			}
		}

		ctx = context.WithValue(ctx, containerd_keys.CONTAINER_CONTEXT_KEY, container)

		return next(ctx, opts, resp, req)
	}
}

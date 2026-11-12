package client

import (
	"context"

	"github.com/cedana/cedana/pkg/types"
	podman_keys "github.com/cedana/cedana/plugins/podman/pkg/keys"
	"github.com/cedana/cedana/plugins/podman/pkg/utils"
	"github.com/rs/zerolog/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func LoadContainer[REQ, RESP any](next types.Handler[REQ, RESP]) types.Handler[REQ, RESP] {
	return func(ctx context.Context, opts types.Opts, resp *RESP, req *REQ) (func() <-chan int, error) {
		details := types.Details(req).GetPodman()

		containers, err := utils.Inspect(ctx, details.ID)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to load container %s: %v", details.ID, err)
		}
		container := containers[0]

		log.Debug().Str("id", container.ID).Str("name", container.Name).Msg("loaded container")

		ctx = context.WithValue(ctx, podman_keys.CONTAINER_CONTEXT_KEY, container)

		return next(ctx, opts, resp, req)
	}
}

package client

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/types"
	podman_keys "github.com/cedana/cedana/plugins/podman/pkg/keys"
)

func CreateContainer(next types.Run) types.Run {
	return func(ctx context.Context, opts types.Opts, resp *daemon.RunResp, req *daemon.RunReq) (code func() <-chan int, err error) {
		if req.Action != daemon.RunAction_START_NEW {
			return next(ctx, opts, resp, req) // nothing to create when managing
		}

		createOpts, err := newCreateOpts(opts, req.GetDetails().GetPodman(), defaultRuntime(ctx))
		if err != nil {
			return nil, err
		}

		ctx = context.WithValue(ctx, podman_keys.CREATE_OPTS_CONTEXT_KEY, createOpts)

		return next(ctx, opts, resp, req)
	}
}

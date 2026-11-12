package client

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/types"
	podman_keys "github.com/cedana/cedana/plugins/podman/pkg/keys"
	"github.com/cedana/cedana/plugins/podman/pkg/utils"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Saves the container's image name in the dump, so restore can use the same one
func DumpImageName(next types.Dump) types.Dump {
	return func(ctx context.Context, opts types.Opts, resp *daemon.DumpResp, req *daemon.DumpReq) (code func() <-chan int, err error) {
		if req.Action != daemon.DumpAction_DUMP || opts.DumpFs == nil {
			return next(ctx, opts, resp, req)
		}

		container, ok := ctx.Value(podman_keys.CONTAINER_CONTEXT_KEY).(*utils.Container)
		if !ok {
			return nil, status.Errorf(codes.Internal, "failed to get container from context")
		}

		file, err := opts.DumpFs.Create(podman_keys.DUMP_IMAGE_NAME_KEY)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to create dump image name file: %v", err)
		}
		defer file.Close()
		_, err = file.WriteString(container.ImageName)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to write dump image name file: %v", err)
		}

		return next(ctx, opts, resp, req)
	}
}

package client

import (
	"context"
	"fmt"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	podman_proto "buf.build/gen/go/cedana/cedana/protocolbuffers/go/plugins/podman"
	runc_proto "buf.build/gen/go/cedana/cedana/protocolbuffers/go/plugins/runc"
	"github.com/cedana/cedana/pkg/features"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/plugins/podman/pkg/utils"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func Query(ctx context.Context, req *daemon.QueryReq) (*daemon.QueryResp, error) {
	query := req.Podman
	if query == nil {
		return nil, status.Errorf(codes.InvalidArgument, "podman query missing")
	}

	ids := query.IDs
	if len(ids) == 0 {
		var err error
		ids, err = utils.List(ctx)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to list containers: %v", err)
		}
	}

	resp := &daemon.QueryResp{Podman: &podman_proto.QueryResp{}}

	for _, id := range ids {
		containers, err := utils.Inspect(ctx, id)
		if err != nil {
			resp.Messages = append(resp.Messages, fmt.Sprintf("%s: failed to inspect: %v", id, err))
			continue
		}
		c := containers[0]

		container := &podman_proto.Podman{
			ID:    c.Name,
			Image: c.ImageName,
		}

		var state *daemon.ProcessState

		// Fetch lower-level runtime info

		plugin, root := utils.PluginForContainer(c)
		if plugin == "" {
			resp.Messages = append(resp.Messages, fmt.Sprintf("%s: unsupported runtime %s", id, c.OCIRuntime))
			continue
		}

		err = features.QueryHandler.IfAvailable(func(_ string, query types.Query) error {
			r, err := query(ctx, &daemon.QueryReq{
				Type: plugin,
				Runc: &runc_proto.QueryReq{
					IDs:  []string{c.ID},
					Root: root,
				},
			})
			if err != nil {
				resp.Messages = append(resp.Messages, fmt.Sprintf("%s: failed to query %s: %v", id, plugin, err))
				return err
			}
			resp.Messages = append(resp.Messages, r.Messages...)
			if len(r.Runc.Containers) > 0 {
				container.Runc = r.Runc.Containers[0]
				state = r.States[0]
			} else {
				return fmt.Errorf("no %s container found for %s", plugin, id)
			}
			return nil
		}, plugin)
		if err == nil {
			resp.Podman.Containers = append(resp.Podman.Containers, container)
			resp.States = append(resp.States, state)
		}
	}

	return resp, nil
}

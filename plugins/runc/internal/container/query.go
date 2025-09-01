package container

// Defines the container query handler

import (
	"context"
	"fmt"
	"strings"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/plugins/runc"
	"github.com/cedana/cedana/plugins/runc/internal/defaults"
	"github.com/opencontainers/runc/libcontainer"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func Query(ctx context.Context, req *daemon.QueryReq) (*daemon.QueryResp, error) {
	query := req.Runc

	if query == nil {
		return nil, status.Errorf(codes.InvalidArgument, "runc query missing")
	}

	root := query.Root

	if root == "" {
		root = defaults.DEFAULT_ROOT
	}

	resp := &daemon.QueryResp{Runc: &runc.QueryResp{}}

	for _, id := range query.IDs {
		container, err := libcontainer.Load(root, id)
		if err != nil {
			resp.Messages = append(resp.Messages, fmt.Sprintf("Container %s: %v", id, err))
			continue
		}

		state, err := container.State()
		if err != nil {
			resp.Messages = append(resp.Messages, fmt.Sprintf("Container %s: %v", id, err))
			continue
		}

		status, err := container.Status()
		if err != nil {
			resp.Messages = append(resp.Messages, fmt.Sprintf("Container %s: %v", id, err))
			continue
		}

		if status != libcontainer.Running {
			resp.Messages = append(resp.Messages,
				fmt.Sprintf("Container %s is not running (status: %s)", id, status))
			continue
		}

		var bundle string
		for _, label := range state.Config.Labels {
			if strings.HasPrefix(label, "bundle") {
				bundle = strings.Split(label, "=")[1]
				break
			}
		}
		if bundle == "" {
			resp.Messages = append(resp.Messages, fmt.Sprintf("Container %s: bundle label not found", id))
		}

		resp.Runc.Containers = append(resp.Runc.Containers, &runc.Runc{
			ID:     container.ID(),
			Bundle: bundle,
			Root:   root,
		})
	}

	if len(resp.Runc.Containers) == 0 {
		return nil, status.Errorf(codes.NotFound, "No containers found in %s", root)
	}

	return resp, nil
}

package client

import (
	"context"
	"fmt"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	containerd_proto "buf.build/gen/go/cedana/cedana/protocolbuffers/go/plugins/containerd"
	runc_proto "buf.build/gen/go/cedana/cedana/protocolbuffers/go/plugins/runc"
	"github.com/cedana/cedana/pkg/features"
	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/plugins/containerd/internal/defaults"
	containerd_utils "github.com/cedana/cedana/plugins/containerd/pkg/utils"
	"github.com/containerd/containerd"
	"github.com/containerd/containerd/namespaces"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func Query(ctx context.Context, req *daemon.QueryReq) (*daemon.QueryResp, error) {
	query := req.Containerd
	if query == nil {
		return nil, status.Errorf(codes.InvalidArgument, "containerd query missing")
	}

	if query.Address == "" {
		query.Address = defaults.DEFAULT_ADDRESS
	}

	if query.Namespace == "" {
		query.Namespace = defaults.DEFAULT_NAMESPACE
	}

	ctx = namespaces.WithNamespace(ctx, query.Namespace)

	client, err := containerd.New(query.Address, containerd.WithDefaultNamespace(query.Namespace))
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create containerd client: %v", err)
	}

	resp := &daemon.QueryResp{Containerd: &containerd_proto.QueryResp{}}

	filters := []string{}
	for _, id := range query.IDs {
		filters = append(filters, "id=="+id)
	}

	containers, err := client.Containers(ctx, filters...)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list containers: %v", err)
	}

	for _, c := range containers {
		info, err := c.Info(ctx)
		if err != nil {
			resp.Messages = append(resp.Messages, fmt.Sprintf("Container %s: failed to get info: %v", c.ID(), err))
			continue
		}

		container := &containerd_proto.Containerd{
			ID:        info.ID,
			Image:     &containerd_proto.Image{Name: info.Image},
			Address:   query.Address,
			Namespace: query.Namespace,
		}

		task, err := c.Task(ctx, nil) // Ensure task is loaded to get accurate info
		if err != nil {
			resp.Messages = append(resp.Messages, fmt.Sprintf("Container %s: failed to get task: %v", c.ID(), err))
			continue
		}

		resp.States = append(resp.States, &daemon.ProcessState{
			PID: task.Pid(),
		})

		// Fetch lower-level runtime info

		runtime := client.Runtime()
		plugin := containerd_utils.PluginForRuntime(runtime)
		root := containerd_utils.RootFromPlugin(plugin, query.Namespace)

		err = features.QueryHandler.IfAvailable(func(_ string, query types.Query) error {
			switch plugin {
			case "runc":
				resp, err := query(ctx, &daemon.QueryReq{
					Type: "runc",
					Runc: &runc_proto.QueryReq{
						IDs:  []string{container.ID},
						Root: root,
					},
				})
				if err != nil {
					return fmt.Errorf("runc query failed: %v", err)
				}
				if len(resp.Runc.Containers) == 0 {
					resp.Messages = append(resp.Messages, fmt.Sprintf("Container %s: runc container not found", container.ID))
					return fmt.Errorf("runc container not found")
				}
				container.Runc = resp.Runc.Containers[0]
			}
			return nil
		}, plugin)
		if err != nil {
			resp.Messages = append(resp.Messages, fmt.Sprintf("Container %s: %v", container.ID, err))
		} else {
		}

		resp.Containerd.Containers = append(resp.Containerd.Containers, container)
	}

	return resp, nil
}

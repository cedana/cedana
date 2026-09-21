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
	"github.com/containerd/containerd/api/types/runc/options"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/typeurl/v2"
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
			resp.Messages = append(resp.Messages, fmt.Sprintf("%s: failed to get info: %v", c.ID(), err))
			continue
		}

		container := &containerd_proto.Containerd{
			ID:        info.ID,
			Image:     &containerd_proto.Image{Name: info.Image},
			Address:   query.Address,
			Namespace: query.Namespace,
		}

		var state *daemon.ProcessState

		// Fetch lower-level runtime info

		runtime := info.Runtime.Name
		var binary string
		if info.Runtime.Options != nil && info.Runtime.Options.GetValue() != nil {
			if v, err := typeurl.UnmarshalAny(info.Runtime.Options); err == nil {
				if o, ok := v.(*options.Options); ok {
					binary = o.BinaryName
				}
			}
		}
		plugin := containerd_utils.PluginForRuntimeBinary(runtime, binary)
		root := containerd_utils.RootFromRuntime(runtime, query.Namespace)

		err = features.QueryHandler.IfAvailable(func(_ string, query types.Query) error {
			r, err := query(ctx, &daemon.QueryReq{
				Type: plugin,
				Runc: &runc_proto.QueryReq{
					IDs:  []string{container.ID},
					Root: root,
				},
			})
			if err != nil {
				resp.Messages = append(resp.Messages, fmt.Sprintf("%s: failed to query %s: %v", container.ID, plugin, err))
				return err
			}
			resp.Messages = append(resp.Messages, r.Messages...)
			if len(r.Runc.Containers) > 0 {
				container.Runc = r.Runc.Containers[0]
				state = r.States[0]
			} else {
				return fmt.Errorf("no %s container found for %s", plugin, container.ID)
			}
			return nil
		}, plugin)
		if err == nil {
			resp.Containerd.Containers = append(resp.Containerd.Containers, container)
			resp.States = append(resp.States, state)
		}
	}

	return resp, nil
}

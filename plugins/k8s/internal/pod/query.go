package pod

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/plugins/k8s/pkg/kube"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func Query(ctx context.Context, req *daemon.QueryReq) (*daemon.QueryResp, error) {
	query := req.K8S
	if query == nil {
		return nil, status.Errorf(codes.InvalidArgument, "k8s query missing")
	}

	client, err := kube.CurrentRuntimeClient()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get current k8s runtime client: %v", err)
	}

	resp, err := client.Pods(ctx, query)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to query k8s pods: %v", err)
	}

	return &daemon.QueryResp{K8S: resp}, nil
}

package pod

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/plugins/k8s/pkg/kube"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func Query(ctx context.Context, req *daemon.QueryReq) (*daemon.QueryResp, error) {
	client, err := kube.CurrentRuntimeClient()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get current k8s runtime client: %v", err)
	}

	return client.Query(ctx, req)
}

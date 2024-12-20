package profiling

import (
	"context"

	"github.com/cedana/cedana/pkg/config"
	"google.golang.org/grpc"
)

func UnaryProfiler() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		if !config.Global.Profiling.Enabled {
			return handler(ctx, req)
		}

		ctx, end := StartTiming(ctx)
		defer end()

		return handler(ctx, req)
	}
}

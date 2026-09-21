package channel

// Defines gRPC interceptors that use channels

import (
	"context"

	"google.golang.org/grpc"
)

// UnaryLifetime creates a gRPC interceptor that cancels the context when the lifetime expires.
// Useful for propagating server shutdown to long-running gRPC calls.
func UnaryLifetime[T any](lifetime <-chan T) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		ctx, cancel := context.WithCancel(ctx)

		go func() {
			select {
			case <-lifetime:
				cancel()
			case <-ctx.Done():
				// gRPC call finished before lifetime expired
			}
		}()

		return handler(ctx, req)
	}
}

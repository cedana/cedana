package logging

// Defines gRPC interceptors for logging

import (
	"context"
	"strings"

	"github.com/rs/zerolog/log"
	"google.golang.org/grpc"
)

func StreamLogger() grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		log.Trace().Str("method", info.FullMethod).Msg("gRPC stream started")

		err := handler(srv, ss)

		if err != nil {
			log.Error().Str("method", info.FullMethod).Err(err).Msg("gRPC stream failed")
		} else {
			log.Trace().Str("method", info.FullMethod).Msg("gRPC stream succeeded")
		}

		return err
	}
}

// TODO NR - this needs a deep copy to properly redact
func UnaryLogger() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		// log the GetContainerInfo method to trace
		if strings.Contains(info.FullMethod, "GetContainerInfo") {
			log.Trace().Str("method", info.FullMethod).Interface("request", req).Msg("gRPC request received")
		} else {
			log.Trace().Str("method", info.FullMethod).Interface("request", req).Msg("gRPC request received")
		}

		resp, err := handler(ctx, req)

		if err != nil {
			log.Error().Str("method", info.FullMethod).Interface("request", req).Interface("response", resp).Err(err).Msg("gRPC request failed")
		} else {
			log.Trace().Str("method", info.FullMethod).Interface("response", resp).Msg("gRPC request succeeded")
		}

		return resp, err
	}
}

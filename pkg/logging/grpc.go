package logging

// Defines gRPC interceptors for logging

import (
	"context"
	"encoding/json"

	"github.com/rs/zerolog/log"
	"google.golang.org/grpc"
)

func StreamLogger() grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
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
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		resp, err := handler(ctx, req)

		if err != nil {
			log.Error().Str("method", info.FullMethod).Interface("request", RedactEnv(req)).Interface("response", RedactEnv(resp)).Err(err).Msg("gRPC request failed")
		} else {
			log.Trace().Str("method", info.FullMethod).Interface("response", RedactEnv(resp)).Msg("gRPC request succeeded")
		}

		return resp, err
	}
}

// RedactEnv recursively removes Env fields for logging
func RedactEnv(v any) any {
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return v // fallback to original if marshal fails
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return v
	}
	delete(m, "Env")
	return m
}

package profiling

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"

	"github.com/cedana/cedana/pkg/config"
	"github.com/cedana/cedana/pkg/keys"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

const PROFILING_METADATA_KEY = "profiling-bin" // bin is required by gRPC when sending binary data

// Sets the profiler data from the context as a trailer in the response.
func UnaryProfiler() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if !config.Global.Profiling.Enabled {
			return handler(ctx, req)
		}

		name := filepath.Base(strings.ToLower(info.FullMethod))

		chilCtx, end := StartTiming(ctx, name)
		resp, err := handler(chilCtx, req)
		end()

		if err != nil {
			return nil, err
		}

		err = AttachTrailer(ctx)
		if err != nil {
			return nil, err
		}

		return resp, nil
	}
}

// Attaches profiling data from the context as a grpc trailer.
func AttachTrailer(ctx context.Context) error {
	data, ok := ctx.Value(keys.PROFILING_CONTEXT_KEY).(*Data)
	if !ok {
		return nil
	}

	CleanData(data)
	FlattenData(data)

	var md metadata.MD
	md, ok = metadata.FromOutgoingContext(ctx)
	if !ok {
		md = make(metadata.MD)
	}

	var buf bytes.Buffer
	err := Encode(data, &buf)
	if err != nil {
		return err
	}

	md.Set(PROFILING_METADATA_KEY, buf.String())

	return grpc.SetTrailer(ctx, md)
}

func FromTrailer(trailer metadata.MD) (*Data, error) {
	data, ok := trailer[PROFILING_METADATA_KEY]
	if !ok {
		return nil, nil
	}

	return Decode(data[0])
}

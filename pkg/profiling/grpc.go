package profiling

import (
	"bytes"
	"context"
	"errors"
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
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		if !config.Global.Profiling.Enabled {
			return handler(ctx, req)
		}

		name := filepath.Base(strings.ToLower(info.FullMethod))

		ctx = context.WithValue(ctx, keys.PROFILING_CONTEXT_KEY, &Data{Name: name})

		chilCtx, end := StartTiming(ctx)
		resp, err := handler(chilCtx, req)
		end()

		return resp, errors.Join(err, AttachData(ctx))
	}
}

// Attaches profiling data from the context as a grpc trailer.
func AttachData(ctx context.Context) error {
	data, ok := ctx.Value(keys.PROFILING_CONTEXT_KEY).(*Data)
	if !ok {
		return nil
	}

	data = &Data{
		Name:       data.Name,
		Duration:   data.Duration,
		Components: data.Components,
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

func GetData(trailer metadata.MD) (*Data, error) {
	data, ok := trailer[PROFILING_METADATA_KEY]
	if !ok {
		return nil, nil
	}

	return Decode(data[0])
}

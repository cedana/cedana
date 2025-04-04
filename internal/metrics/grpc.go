package metrics

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/config"
	"github.com/cedana/cedana/pkg/utils"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
)

const API_TRACER = "cedana/api"

func UnaryTracer(host *daemon.Host) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		if !config.Global.Metrics.Otel {
			return handler(ctx, req)
		}

		tracer := otel.Tracer(API_TRACER)

		ctx, span := tracer.Start(ctx, info.FullMethod, trace.WithSpanKind(trace.SpanKindServer))
		resp, err := handler(ctx, req)
		span.End()

		span.SetAttributes(
			attribute.String("grpc.method", info.FullMethod),
			attribute.String("grpc.request", utils.ProtoToJSON(req)),
			attribute.String("grpc.response", utils.ProtoToJSON(resp)),
			attribute.String("server.id", host.ID),
			attribute.String("server.mac", host.MAC),
			attribute.String("server.hostname", host.Hostname),
			attribute.String("config.connection.url", config.Global.Connection.URL),
		)

		if err != nil {
			span.SetStatus(codes.Error, err.Error())
			span.RecordError(err)
		}

		return resp, err
	}
}

func StreamTracer(host *daemon.Host) grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if !config.Global.Metrics.Otel {
			return handler(srv, ss)
		}

		tracer := otel.Tracer(API_TRACER)

		ctx := ss.Context()
		ctx, span := tracer.Start(ctx, info.FullMethod, trace.WithSpanKind(trace.SpanKindServer))
		err := handler(srv, ss)
		span.End()

		span.SetAttributes(
			attribute.String("grpc.method", info.FullMethod),
			attribute.String("server.id", host.ID),
			attribute.String("server.mac", host.MAC),
			attribute.String("server.hostname", host.Hostname),
			attribute.String("config.connection.url", config.Global.Connection.URL),
		)

		if err != nil {
			span.SetStatus(codes.Error, err.Error())
			span.RecordError(err)
		}

		return err
	}
}

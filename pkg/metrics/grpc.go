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

const TRACER_NAME = "cedana/daemon"

func UnaryTracer(host *daemon.Host) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if !config.Global.Metrics {
			return handler(ctx, req)
		}

		tracer := otel.Tracer(TRACER_NAME)

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
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if !config.Global.Metrics {
			return handler(srv, ss)
		}

		tracer := otel.Tracer(TRACER_NAME)

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

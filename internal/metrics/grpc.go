package metrics

import (
	"context"

	"github.com/cedana/cedana/pkg/config"
	"github.com/cedana/cedana/pkg/utils"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
)

func UnaryTracer(machine utils.Machine) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		tp := otel.GetTracerProvider()
		tracer := tp.Tracer("cedana/api")

		ctx, span := tracer.Start(ctx, info.FullMethod, trace.WithSpanKind(trace.SpanKindServer))
		resp, err := handler(ctx, req)
		span.End()

		span.SetAttributes(
			attribute.String("grpc.method", info.FullMethod),
			attribute.String("grpc.request", utils.ProtoToJSON(req)),
			attribute.String("grpc.response", utils.ProtoToJSON(resp)),
			attribute.String("server.id", machine.ID),
			attribute.String("server.mac", machine.MACAddr),
			attribute.String("server.hostname", machine.Hostname),
			attribute.String("server.opts.cedanaurl", config.Global.Connection.URL),
		)

		if err != nil {
			span.SetStatus(codes.Error, err.Error())
			span.RecordError(err)
		}

		return resp, err
	}
}

func StreamTracer(machine utils.Machine) grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		tp := otel.GetTracerProvider()
		tracer := tp.Tracer("cedana/api")

		ctx := ss.Context()
		ctx, span := tracer.Start(ctx, info.FullMethod, trace.WithSpanKind(trace.SpanKindServer))
		err := handler(srv, ss)
		span.End()

		span.SetAttributes(
			attribute.String("grpc.method", info.FullMethod),
			attribute.String("server.id", machine.ID),
			attribute.String("server.mac", machine.MACAddr),
			attribute.String("server.hostname", machine.Hostname),
			attribute.String("server.opts.cedanaurl", config.Global.Connection.URL),
		)

		if err != nil {
			span.SetStatus(codes.Error, err.Error())
			span.RecordError(err)
		}

		return err
	}
}

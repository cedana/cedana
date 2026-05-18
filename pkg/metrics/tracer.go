package metrics

import (
	"context"
	"fmt"
	"sync"

	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	"google.golang.org/grpc/credentials"
)

// initTracer creates and configures an OpenTelemetry trace provider for SigNoz
func initTracer(ctx context.Context, wg *sync.WaitGroup, resource *resource.Resource) error {
	secureOption := otlptracegrpc.WithTLSCredentials(credentials.NewClientTLSFromCert(nil, ""))

	traceExporter, err := otlptrace.New(
		ctx,
		otlptracegrpc.NewClient(
			secureOption,
			otlptracegrpc.WithEndpoint(Credentials.Endpoint),
			otlptracegrpc.WithHeaders(map[string]string{
				"signoz-ingestion-key": Credentials.Headers,
			}),
		))
	if err != nil {
		return fmt.Errorf("failed to create trace exporter: %w", err)
	}

	traceProvider := trace.NewTracerProvider(
		trace.WithBatcher(traceExporter),
		trace.WithResource(resource),
	)

	otel.SetTracerProvider(traceProvider)

	wg.Add(1)
	go func() {
		defer wg.Done()
		<-ctx.Done()
		if err := traceProvider.Shutdown(context.WithoutCancel(ctx)); err != nil {
			log.Warn().Str("endpoint", Credentials.Endpoint).Err(err).Msg("tracing shutdown failed")
		} else {
			log.Debug().Str("endpoint", Credentials.Endpoint).Msg("tracing shutdown")
		}
	}()

	log.Debug().Str("endpoint", Credentials.Endpoint).Msg("tracing initialized")

	return nil
}

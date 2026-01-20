package metrics

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	"google.golang.org/grpc/credentials"
)

const METER_INTERVAL = 30 * time.Second

// initMeter creates and configures an OpenTelemetry meter provider for SigNoz
func initMeter(ctx context.Context, wg *sync.WaitGroup, resource *resource.Resource) error {
	secureOption := otlpmetricgrpc.WithTLSCredentials(credentials.NewClientTLSFromCert(nil, ""))

	metricExporter, err := otlpmetricgrpc.New(
		ctx,
		secureOption,
		otlpmetricgrpc.WithEndpoint(Credentials.Endpoint),
		otlpmetricgrpc.WithHeaders(map[string]string{
			"signoz-ingestion-key": Credentials.Headers,
		}),
	)
	if err != nil {
		return fmt.Errorf("failed to create metric exporter: %w", err)
	}

	meterProvider := metric.NewMeterProvider(
		metric.WithReader(metric.NewPeriodicReader(metricExporter, metric.WithInterval(METER_INTERVAL))),
		metric.WithResource(resource),
	)

	otel.SetMeterProvider(meterProvider)

	wg.Add(1)
	go func() {
		defer wg.Done()
		<-ctx.Done()
		if err := meterProvider.Shutdown(context.WithoutCancel(ctx)); err != nil {
			log.Warn().Str("endpoint", Credentials.Endpoint).Err(err).Msg("metrics shutdown failed")
		} else {
			log.Debug().Str("endpoint", Credentials.Endpoint).Msg("metrics shutdown")
		}
	}()

	log.Debug().Str("endpoint", Credentials.Endpoint).Msg("metrics initialized")

	return nil
}

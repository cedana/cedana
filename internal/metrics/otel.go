package metrics

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"

	"github.com/cedana/cedana/pkg/config"
	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
	"google.golang.org/grpc/credentials"
)

// setupOTelSDK bootstraps the OpenTelemetry pipeline.
// If it does not return an error, make sure to call shutdown for proper cleanup.
func InitOtel(ctx context.Context, version string) (shutdown func(context.Context) error, err error) {
	var shutdownFuncs []func(context.Context) error

	shutdown = func(ctx context.Context) error {
		var err error
		for _, fn := range shutdownFuncs {
			err = errors.Join(err, fn(ctx))
		}
		shutdownFuncs = nil
		return err
	}

	handleErr := func(inErr error) {
		err = errors.Join(inErr, shutdown(ctx))
		log.Debug().Err(err).Msg("failed to set up otel, will use noop")
	}

	prop := newPropagator()
	otel.SetTextMapPropagator(prop)

	endpoint, headers, err := getOtelCreds()
	if err != nil {
		handleErr(err)
		return
	}

	tracerProvider, err := newTracerProvider(ctx, version, endpoint, headers)
	if err != nil {
		handleErr(err)
		return
	}

	shutdownFuncs = append(shutdownFuncs, tracerProvider.Shutdown)
	otel.SetTracerProvider(tracerProvider)

	log.Info().Str("endpoint", endpoint).Msg("otel initialized")

	return
}

func getOtelCreds() (string, string, error) {
	url := config.Global.Connection.URL
	authToken := config.Global.Connection.AuthToken
	if url == "" || authToken == "" {
		return "", "", fmt.Errorf("connection URL or AuthToken unset in config/env")
	}

	url = url + "/otel/credentials"
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", authToken))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("failed to fetch otel credentials, status code: %d", resp.StatusCode)
	}

	var creds struct {
		Endpoint string `json:"OTEL_EXPORTER_OTLP_ENDPOINT"`
		Headers  string `json:"OTEL_EXPORTER_OTLP_HEADERS"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&creds); err != nil {
		return "", "", err
	}

	return creds.Endpoint, creds.Headers, nil
}

func newPropagator() propagation.TextMapPropagator {
	return propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	)
}

func newTracerProvider(ctx context.Context, version, endpoint, headers string) (*trace.TracerProvider, error) {
	// set headers env var
	if err := os.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "signoz-ingestion-key="+headers); err != nil {
		return nil, err
	}

	secureOption := otlptracegrpc.WithTLSCredentials(credentials.NewClientTLSFromCert(nil, ""))

	traceExporter, err := otlptrace.New(
		ctx,
		otlptracegrpc.NewClient(
			secureOption,
			otlptracegrpc.WithEndpoint(endpoint),
		))
	if err != nil {
		return nil, err
	}

	resources, err := resource.New(
		ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String("cedana"),
			semconv.ServiceVersionKey.String(version),
			attribute.KeyValue{
				Key:   "cedana.service.url",
				Value: attribute.StringValue(config.Global.Connection.URL),
			},
		),
	)
	if err != nil {
		return nil, err
	}

	traceProvider := trace.NewTracerProvider(
		trace.WithBatcher(traceExporter),
		trace.WithResource(resources),
	)
	return traceProvider, nil
}

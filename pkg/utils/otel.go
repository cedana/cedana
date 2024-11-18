package utils

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/viper"
	"github.com/vincentfree/opentelemetry/otelzerolog"
	"github.com/vincentfree/opentelemetry/providerconfig"
	"github.com/vincentfree/opentelemetry/providerconfig/providerconfighttp"
	otelzerologbridge "go.opentelemetry.io/contrib/bridges/otelzerolog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
	"go.opentelemetry.io/otel/trace/noop"
	"google.golang.org/grpc/credentials"
)

// setupOTelSDK bootstraps the OpenTelemetry pipeline.
// If it does not return an error, make sure to call shutdown for proper cleanup.
func InitOtel(ctx context.Context, version string) (shutdown func(context.Context) error, err error) {
	log.Info().Msg("initializing standard otel tracer")
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
	}

	prop := newPropagator()
	otel.SetTextMapPropagator(prop)

	endpoint, headers, err := getOtelCreds()
	if err != nil {
		handleErr(err)
		return
	}

	tracerProvider, err := newTraceProvider(ctx, version, endpoint, headers)
	if err != nil {
		handleErr(err)
		return
	}
	shutdownFuncs = append(shutdownFuncs, tracerProvider.Shutdown)
	otel.SetTracerProvider(tracerProvider)

	if logProvider("cedana-daemon", "unknown-dev", endpoint, headers) != nil {
		handleErr(err)
		return
	}
	return
}

func getOtelCreds() (string, string, error) {
	cedanaURL := viper.GetString("connection.cedana_url")
	if cedanaURL == "" {
		return "", "", fmt.Errorf("CEDANA_URL or CEDANA_AUTH_TOKEN unset, cannot fetch otel credentials")
	}

	url := cedanaURL + "/k8s/otelcreds"
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", viper.GetString("connection.cedana_auth_token")))

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

func InitOtelNoop() {
	log.Info().Msg("using noop tracer provider")
	otel.SetTracerProvider(noop.NewTracerProvider())
}

func newPropagator() propagation.TextMapPropagator {
	return propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	)
}

func newTraceProvider(ctx context.Context, version, endpoint, headers string) (*trace.TracerProvider, error) {
	// set headers env var
	if err := os.Setenv("OTEL_EXPORTER_OTLP_HEADERS", headers); err != nil {
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
func logProvider(serviceName, version, endpoint, headers string) error {
	_ = os.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	_ = os.Setenv("OTEL_EXPORTER_OTLP_HEADERS", headers)
	signalProcessor := providerconfighttp.New(
		providerconfighttp.WithLogOptions(otlploghttp.WithEndpoint(endpoint)),
	)
	provider := providerconfig.New(
		providerconfig.WithApplicationName(serviceName),
		providerconfig.WithApplicationVersion(version),
		providerconfig.WithSignalProcessor(signalProcessor),
	)

	otelzerolog.SetGlobalLogger(
		otelzerolog.WithOtelBridge(
			serviceName,
			otelzerologbridge.WithLoggerProvider(provider.LogProvider()),
			otelzerologbridge.WithVersion("0.1.0"),
		),
		otelzerolog.WithAttributes(
			attribute.Bool("production", os.Getenv("CEDANA_DEBUG") == ""),
		),
		otelzerolog.WithServiceName(serviceName),
		otelzerolog.WithZeroLogFeatures(
			zerolog.Context.Stack,
			zerolog.Context.Caller,
			zerolog.Context.Timestamp,
		),
	)
	return nil
}

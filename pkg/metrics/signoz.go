package metrics

import (
	"context"
	"fmt"
	"os"
	"sync"

	propagatorsdk "github.com/cedana/cedana-propagator-sdk/go"
	"github.com/cedana/cedana/pkg/config"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
)

// Creds holds OpenTelemetry exporter credentials
type Creds struct {
	Endpoint string `json:"OTEL_EXPORTER_OTLP_ENDPOINT"`
	Headers  string `json:"OTEL_EXPORTER_OTLP_HEADERS"`
}

var Credentials *Creds

// Init initializes OpenTelemetry tracing and metrics with SigNoz as the backend.
// Extra resource attributes (e.g. installed plugin versions, see
// ResourceAttributes) are attached to every log, trace and metric emitted.
func Init(ctx context.Context, wg *sync.WaitGroup, service, version string, extra ...attribute.KeyValue) {
	log := log.With().Str("service", service).Str("version", version).Logger()

	handleErr := func(err error) {
		log.Warn().Err(err).Msg("metrics will not be sent to SigNoz")
	}

	initPropagator()

	err := getCreds(ctx)
	if err != nil {
		handleErr(err)
		return
	}

	log = log.With().Str("endpoint", Credentials.Endpoint).Logger()

	// The OTLP exporters read OTEL_EXPORTER_OTLP_* from the environment before
	// applying our options. We configure them from the credentials fetched above,
	// so drop any inherited values to avoid conflicting or malformed settings.
	for _, k := range []string{
		"OTEL_EXPORTER_OTLP_ENDPOINT",
		"OTEL_EXPORTER_OTLP_HEADERS",
		"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT",
		"OTEL_EXPORTER_OTLP_TRACES_HEADERS",
		"OTEL_EXPORTER_OTLP_METRICS_ENDPOINT",
		"OTEL_EXPORTER_OTLP_METRICS_HEADERS",
	} {
		os.Unsetenv(k)
	}

	host, err := utils.GetHost(ctx)
	if err != nil {
		handleErr(err)
		return
	}

	attrs := []attribute.KeyValue{
		semconv.HostNameKey.String(host.Hostname),
		semconv.HostIDKey.String(host.ID),
		semconv.HostArchKey.String(host.KernelArch),
		semconv.ServiceNameKey.String(service),
		semconv.ServiceVersionKey.String(version),
		semconv.K8SClusterNameKey.String(config.Global.Connection.ClusterID),
		semconv.K8SNodeNameKey.String(host.Hostname),
		attribute.KeyValue{Key: "cedana.service.url", Value: attribute.StringValue(config.Global.Connection.URL)},
		attribute.KeyValue{Key: "cluster.id", Value: attribute.StringValue(config.Global.Connection.ClusterID)},
	}
	attrs = append(attrs, extra...)

	resource, err := resource.New(ctx, resource.WithAttributes(attrs...))
	if err != nil {
		handleErr(err)
		return
	}

	err = initLogger(ctx, wg, resource)
	if err != nil {
		handleErr(err)
		return
	}

	err = initTracer(ctx, wg, resource)
	if err != nil {
		handleErr(err)
		return
	}

	err = initMeter(ctx, wg, resource)
	if err != nil {
		handleErr(err)
		return
	}
}

// ResourceAttributes converts a map of attribute key to value into OpenTelemetry
// resource attributes, suitable for passing to Init.
func ResourceAttributes(m map[string]string) []attribute.KeyValue {
	attrs := make([]attribute.KeyValue, 0, len(m))
	for k, v := range m {
		attrs = append(attrs, attribute.String(k, v))
	}
	return attrs
}

// getCreds fetches OpenTelemetry credentials from the Cedana endpoint
func getCreds(ctx context.Context) error {
	url := config.Global.Connection.URL
	authToken := config.Global.Connection.AuthToken
	if url == "" || authToken == "" {
		return fmt.Errorf("connection URL or AuthToken unset in config/env")
	}

	creds, err := propagatorsdk.NewClient(url, authToken).V1().Otel().Credentials().Get(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to fetch otel credentials: %w", err)
	}

	endpoint := creds.GetOTELEXPORTEROTLPENDPOINT()
	headers := creds.GetOTELEXPORTEROTLPHEADERS()
	if endpoint == nil || headers == nil || *endpoint == "" || *headers == "" {
		return fmt.Errorf("received incomplete credentials from server")
	}

	Credentials = &Creds{Endpoint: *endpoint, Headers: *headers}

	return nil
}

// initPropagator initializes the OpenTelemetry propagator for context propagation
func initPropagator() {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
}

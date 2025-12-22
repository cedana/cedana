package metrics

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

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
// Returns a shutdown function that must be called for proper cleanup.
func Init(ctx context.Context, wg *sync.WaitGroup, service, version string) {
	log := log.With().Str("service", service).Str("version", version).Logger()

	handleErr := func(err error) {
		log.Warn().Err(err).Msg("metrics will not be sent to SigNoz")
	}

	initPropagator()

	err := getCreds()
	if err != nil {
		handleErr(err)
		return
	}

	log = log.With().Str("endpoint", Credentials.Endpoint).Logger()

	host, err := utils.GetHost(ctx)
	if err != nil {
		handleErr(err)
		return
	}

	resource, err := resource.New(
		ctx,
		resource.WithAttributes(
			semconv.HostNameKey.String(host.Hostname),
			semconv.HostIDKey.String(host.ID),
			semconv.HostArchKey.String(host.KernelArch),
			semconv.ServiceNameKey.String(service),
			semconv.ServiceVersionKey.String(version),
			semconv.K8SClusterNameKey.String(config.Global.ClusterID),
			semconv.K8SNodeNameKey.String(host.Hostname),
			semconv.K8SNodeNameKey.String(host.Hostname),
			attribute.KeyValue{Key: "cedana.service.url", Value: attribute.StringValue(config.Global.Connection.URL)},
			attribute.KeyValue{Key: "cluster.id", Value: attribute.StringValue(config.Global.ClusterID)},
		),
	)
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

// getCreds fetches OpenTelemetry credentials from the Cedana endpoint
func getCreds() error {
	url := config.Global.Connection.URL
	authToken := config.Global.Connection.AuthToken
	if url == "" || authToken == "" {
		return fmt.Errorf("connection URL or AuthToken unset in config/env")
	}

	credentialsURL := url + "/otel/credentials"
	req, err := http.NewRequest(http.MethodGet, credentialsURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", authToken))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to fetch credentials: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to fetch otel credentials, status code: %d", resp.StatusCode)
	}

	Credentials = &Creds{}
	if err := json.NewDecoder(resp.Body).Decode(&Credentials); err != nil {
		return fmt.Errorf("failed to decode credentials: %w", err)
	}

	if Credentials.Endpoint == "" || Credentials.Headers == "" {
		return fmt.Errorf("received incomplete credentials from server")
	}

	return nil
}

// initPropagator initializes the OpenTelemetry propagator for context propagation
func initPropagator() {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
}

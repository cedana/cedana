package metrics

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/cedana/cedana/pkg/logging"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/otel/sdk/resource"
)

const (
	LOGGER_MAX_BATCH_SIZE = 100
	LOGGER_FLUSH_INTERVAL = 5 * time.Second
)

type signozLogEntry struct {
	Timestamp      int64             `json:"timestamp"` // Unix nanoseconds
	TraceID        string            `json:"trace_id,omitempty"`
	SpanID         string            `json:"span_id,omitempty"`
	TraceFlags     uint32            `json:"trace_flags,omitempty"` // Typically 0 or 1 (sampled)
	SeverityText   string            `json:"severity_text"`
	SeverityNumber int32             `json:"severity_number"`
	Body           string            `json:"body"`
	Attributes     map[string]string `json:"attributes,omitempty"`
	Resources      map[string]string `json:"resources,omitempty"`
}

// mapZerologLevelToSigNoz maps zerolog levels to SigNoz/OTel severity.
func mapZerologLevelToSigNoz(level zerolog.Level) (string, int32) {
	switch level {
	case zerolog.TraceLevel:
		return "trace", 1
	case zerolog.DebugLevel:
		return "debug", 5
	case zerolog.InfoLevel:
		return "info", 9
	case zerolog.WarnLevel:
		return "warn", 13
	case zerolog.ErrorLevel:
		return "error", 17
	case zerolog.FatalLevel:
		return "fatal", 21
	case zerolog.PanicLevel:
		return "fatal", 21 // OTel doesn't have Panic, map to FATAL
	case zerolog.NoLevel, zerolog.Disabled:
		return "unknown", 0
	default:
		return strings.ToLower(level.String()), 0 // Best effort
	}
}

// signozWriter implements io.Writer to send logs to SigNoz /logs/json endpoint
type signozWriter struct {
	httpClient  *http.Client
	endpoint    string
	accessToken string
	resource    map[string]string
	logBuffer   []signozLogEntry

	mu sync.Mutex
}

func initLogger(ctx context.Context, wg *sync.WaitGroup, resource *resource.Resource) error {
	if Credentials == nil {
		return fmt.Errorf("credentials not found")
	}

	sw := &signozWriter{
		httpClient:  &http.Client{Timeout: 15 * time.Second}, // Increased timeout slightly for batch
		endpoint:    "https://" + Credentials.Endpoint + ":443/logs/json",
		accessToken: Credentials.Headers,
		resource:    make(map[string]string),
		logBuffer:   make([]signozLogEntry, 0, LOGGER_MAX_BATCH_SIZE),
	}

	if resource != nil {
		attrs := resource.Attributes()
		for _, attr := range attrs {
			sw.resource[string(attr.Key)] = attr.Value.AsString()
		}
	}

	ticker := time.NewTicker(LOGGER_FLUSH_INTERVAL)

	wg.Go(func() {
		for {
			select {
			case <-ticker.C:
				sw.flushBuffer()
			case <-ctx.Done():
				log.Debug().Str("endpoint", Credentials.Endpoint).Msg("logging shutdown")

				ticker.Stop()
				sw.flushBuffer()
				return
			}
		}
	})

	logging.AddLogger(sw)

	log.Debug().Str("endpoint", Credentials.Endpoint).Msg("logging initialized")

	return nil
}

func (sw *signozWriter) Write(p []byte) (n int, err error) {
	var zerologEntry map[string]any
	if err := json.Unmarshal(p, &zerologEntry); err != nil {
		fmt.Printf("error unmarshalling zerolog entry: %v\n", err)
		return len(p), nil // Consume and drop
	}

	var tsNano int64 = time.Now().UnixNano() // Default to now

	levelStr, _ := zerologEntry[zerolog.LevelFieldName].(string)
	parsedLevel, _ := zerolog.ParseLevel(levelStr) // Handles error by defaulting to NoLevel
	severityText, severityNumber := mapZerologLevelToSigNoz(parsedLevel)

	body, _ := zerologEntry[zerolog.MessageFieldName].(string)
	error, _ := zerologEntry[zerolog.ErrorFieldName].(string)
	if error != "" {
		body = fmt.Sprintf("%s: %s", body, error) // Append error if present
	}

	attributes := make(map[string]string)

	for k, v := range zerologEntry {
		if k == zerolog.TimestampFieldName || k == zerolog.LevelFieldName || k == zerolog.MessageFieldName || k == zerolog.ErrorFieldName {
			continue
		}
		switch k {
		case zerolog.CallerFieldName:
			attributes["code.filepath"], _ = v.(string)
		case zerolog.ErrorStackFieldName:
			attributes["exception.stacktrace"], _ = v.(string)
		default:
			attributes[k] = fmt.Sprintf("%v", v)
		}
	}

	logEntry := signozLogEntry{
		Timestamp:      tsNano,
		SeverityText:   severityText,
		SeverityNumber: severityNumber,
		Body:           body,
		Attributes:     attributes,
		Resources:      sw.resource,
	}

	sw.mu.Lock()
	sw.logBuffer = append(sw.logBuffer, logEntry)
	sw.mu.Unlock()

	return len(p), nil
}

func (sw *signozWriter) flushBuffer() {
	sw.mu.Lock()
	if len(sw.logBuffer) == 0 {
		sw.mu.Unlock()
		return
	}
	batchToSend := sw.logBuffer
	sw.logBuffer = make([]signozLogEntry, 0, LOGGER_MAX_BATCH_SIZE)
	sw.mu.Unlock()

	jsonData, err := json.Marshal(batchToSend)
	if err != nil {
		fmt.Printf("error marshalling log batch: %v\n", err)
		return
	}

	req, err := http.NewRequest("POST", sw.endpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		fmt.Printf("error creating HTTP request for log batch: %v\n", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("signoz-ingestion-key", sw.accessToken)

	resp, err := sw.httpClient.Do(req)
	if err != nil {
		fmt.Printf("error sending log batch: %v\n", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		fmt.Printf("non-2xx status sending log batch: %d, response: %s\n", resp.StatusCode, string(bodyBytes))
	}
}


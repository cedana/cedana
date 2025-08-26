package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/cedana/cedana/internal/version"
	"github.com/cedana/cedana/pkg/config"
	"github.com/cedana/cedana/pkg/metrics"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/rs/zerolog"
)

const (
	DEFAULT_SERVICE_NAME           = "cedana"
	DEFAULT_MAX_BATCH_SIZE_JSON    = 100
	DEFAULT_FLUSH_INTERVAL_MS_JSON = 5000 // 5 seconds
)

type SigNozLogEntry struct {
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

// SigNozWriter implements io.Writer to send logs to SigNoz /logs/json endpoint
type SigNozWriter struct {
	httpClient    *http.Client
	endpoint      string
	accessToken   string
	resourceAttrs map[string]string

	mu            sync.Mutex
	logBuffer     []SigNozLogEntry
	maxBatchSize  int
	flushInterval time.Duration
	ticker        *time.Ticker
	lifetime      context.Context
	wg            *sync.WaitGroup
}

func NewSigNozWriter(ctx context.Context, wg *sync.WaitGroup) (*SigNozWriter, error) {
	endpoint, token, err := metrics.GetOtelCreds()
	if err != nil {
		return nil, err
	}

	host, err := utils.GetHost(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get host info: %w", err)
	}
	clusterId, _ := os.LookupEnv("CEDANA_CLUSTER_ID")
	cedanaUrl := config.Global.Connection.URL
	version := version.GetVersion()

	resources := map[string]string{
		"host.name":          host.Hostname,
		"cluster.id":         clusterId,
		"cedana.service.url": cedanaUrl,
		"version":            version,
		"service.name":       DEFAULT_SERVICE_NAME,
	}

	sw := &SigNozWriter{
		httpClient:    &http.Client{Timeout: 15 * time.Second}, // Increased timeout slightly for batch
		endpoint:      "https://" + endpoint + ":443/logs/json",
		accessToken:   token,
		resourceAttrs: resources,
		logBuffer:     make([]SigNozLogEntry, 0, DEFAULT_MAX_BATCH_SIZE_JSON),
		maxBatchSize:  DEFAULT_MAX_BATCH_SIZE_JSON,
		flushInterval: time.Duration(DEFAULT_FLUSH_INTERVAL_MS_JSON) * time.Millisecond,
		lifetime:      ctx,
		wg:            wg,
	}

	if sw.endpoint == "" || sw.accessToken == "" {
		return sw, fmt.Errorf("endpoint or access token missing")
	}

	sw.ticker = time.NewTicker(sw.flushInterval)
	sw.wg.Add(1)
	go sw.runSender()

	return sw, nil
}

func (sw *SigNozWriter) Write(p []byte) (n int, err error) {
	if sw.endpoint == "" || sw.accessToken == "" {
		return len(p), nil
	}

	var zerologEntry map[string]any
	if err := json.Unmarshal(p, &zerologEntry); err != nil {
		fmt.Fprintf(os.Stderr, "signoz: Error unmarshalling zerolog entry: %v\nOriginal log: %s\n", err, string(p))
		return len(p), nil // Consume and drop
	}

	// Timestamp
	var tsNano int64 = time.Now().UnixNano() // Default to now
	if tsStr, ok := zerologEntry[zerolog.TimestampFieldName].(string); ok {
		parsedTime, err := time.Parse(ZEROLOG_TIME_FORMAT_DEFAULT, tsStr)
		if err == nil {
			tsNano = parsedTime.UnixNano()
		} else {
			fmt.Fprintf(os.Stderr, "signoz: Error parsing timestamp: %v. Using current time.\n", err)
		}
	}

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
		if k == zerolog.TimestampFieldName || k == zerolog.LevelFieldName || k == zerolog.MessageFieldName {
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

	logEntry := SigNozLogEntry{
		Timestamp:      tsNano,
		SeverityText:   severityText,
		SeverityNumber: severityNumber,
		Body:           body,
		Attributes:     attributes,
		Resources:      sw.resourceAttrs, // Use pre-configured resource attributes
	}

	sw.mu.Lock()
	sw.logBuffer = append(sw.logBuffer, logEntry)
	sw.mu.Unlock()

	return len(p), nil
}

func (sw *SigNozWriter) runSender() {
	defer sw.wg.Done()
	for {
		select {
		case <-sw.ticker.C:
			sw.flushBuffer()
		case <-sw.lifetime.Done():
			sw.ticker.Stop()
			sw.flushBuffer() // Final flush
			return
		}
	}
}

func (sw *SigNozWriter) flushBuffer() {
	sw.mu.Lock()
	if len(sw.logBuffer) == 0 {
		sw.mu.Unlock()
		return
	}
	batchToSend := make([]SigNozLogEntry, len(sw.logBuffer))
	copy(batchToSend, sw.logBuffer)
	sw.logBuffer = make([]SigNozLogEntry, 0, sw.maxBatchSize) // Clear buffer
	sw.mu.Unlock()

	if len(batchToSend) == 0 {
		return
	}

	jsonData, err := json.Marshal(batchToSend)
	if err != nil {
		fmt.Fprintf(os.Stderr, "signoz: Error marshalling log batch: %v\n", err)
		return
	}

	req, err := http.NewRequest("POST", sw.endpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		fmt.Fprintf(os.Stderr, "signoz: Error creating HTTP request: %v\n", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("signoz-ingestion-key", sw.accessToken)

	resp, err := sw.httpClient.Do(req)
	if err != nil {
		// TODO NR - add backoff?
		fmt.Fprintf(os.Stderr, "signoz: Error sending log batch: %v\n", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		fmt.Fprintf(os.Stderr, "signoz: returned non-2xx status: %d. Response: %s\n", resp.StatusCode, string(bodyBytes))
	} else {
	}
}

package logging

import (
	"bytes"
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
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
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
		return "TRACE", 1
	case zerolog.DebugLevel:
		return "DEBUG", 5
	case zerolog.InfoLevel:
		return "INFO", 9
	case zerolog.WarnLevel:
		return "WARN", 13
	case zerolog.ErrorLevel:
		return "ERROR", 17
	case zerolog.FatalLevel:
		return "FATAL", 21
	case zerolog.PanicLevel:
		return "FATAL", 21 // OTel doesn't have Panic, map to FATAL
	case zerolog.NoLevel, zerolog.Disabled:
		return "UNKNOWN", 0
	default:
		return strings.ToUpper(level.String()), 0 // Best effort
	}
}

// SigNozJsonWriter implements io.Writer to send logs to SigNoz /logs/json endpoint
type SigNozJsonWriter struct {
	httpClient    *http.Client
	endpoint      string
	accessToken   string
	resourceAttrs map[string]string

	mu            sync.Mutex
	logBuffer     []SigNozLogEntry
	maxBatchSize  int
	flushInterval time.Duration
	ticker        *time.Ticker
	doneChan      chan struct{}
	wg            sync.WaitGroup
}

func NewSigNozJsonWriter(endpoint, token, serviceName string, otherResourceAttrs map[string]string, maxBatchSize int, flushIntervalMs int) *SigNozJsonWriter {
	resources := make(map[string]string)
	if otherResourceAttrs != nil {
		for k, v := range otherResourceAttrs {
			resources[k] = v
		}
	}
	resources["service.name"] = "cedana"

	sw := &SigNozJsonWriter{
		httpClient:    &http.Client{Timeout: 15 * time.Second}, // Increased timeout slightly for batch
		endpoint:      endpoint,
		accessToken:   token,
		resourceAttrs: resources,
		logBuffer:     make([]SigNozLogEntry, 0, maxBatchSize),
		maxBatchSize:  maxBatchSize,
		flushInterval: time.Duration(flushIntervalMs) * time.Millisecond,
		doneChan:      make(chan struct{}),
	}

	if sw.endpoint != "" && sw.accessToken != "" {
		sw.ticker = time.NewTicker(sw.flushInterval)
		sw.wg.Add(1)
		go sw.runSender()
	} else {
		fmt.Fprintln(os.Stderr, "SigNozJsonWriter: Endpoint or Access Token not provided. SigNoz logging will be disabled for this writer.")
	}

	return sw
}

func (sw *SigNozJsonWriter) Write(p []byte) (n int, err error) {
	if sw.endpoint == "" || sw.accessToken == "" {
		return len(p), nil
	}

	var zerologEntry map[string]any
	if err := json.Unmarshal(p, &zerologEntry); err != nil {
		fmt.Fprintf(os.Stderr, "SigNozJsonWriter: Error unmarshalling zerolog entry: %v\nOriginal log: %s\n", err, string(p))
		return len(p), nil // Consume and drop
	}

	// Timestamp
	var tsNano int64 = time.Now().UnixNano() // Default to now
	if tsStr, ok := zerologEntry[zerolog.TimestampFieldName].(string); ok {
		parsedTime, err := time.Parse(ZEROLOG_TIME_FORMAT_DEFAULT, tsStr)
		if err == nil {
			tsNano = parsedTime.UnixNano()
		} else {
			fmt.Fprintf(os.Stderr, "SigNozJsonWriter: Error parsing timestamp: %v. Using current time.\n", err)
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
	attributes["version"] = version.GetVersion()
	attributes["cedana.service.url"] = config.Global.Connection.URL

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

func (sw *SigNozJsonWriter) runSender() {
	defer sw.wg.Done()
	for {
		select {
		case <-sw.ticker.C:
			sw.flushBuffer()
		case <-sw.doneChan:
			sw.ticker.Stop()
			sw.flushBuffer() // Final flush
			return
		}
	}
}

func (sw *SigNozJsonWriter) flushBuffer() {
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
		fmt.Fprintf(os.Stderr, "SigNozJsonWriter: Error marshalling log batch: %v\n", err)
		return
	}

	req, err := http.NewRequest("POST", sw.endpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		fmt.Fprintf(os.Stderr, "SigNozJsonWriter: Error creating HTTP request: %v\n", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("signoz-ingestion-key", sw.accessToken)

	resp, err := sw.httpClient.Do(req)
	if err != nil {
		// TODO NR - add backoff?
		fmt.Fprintf(os.Stderr, "SigNozJsonWriter: Error sending log batch to SigNoz: %v\n", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		fmt.Fprintf(os.Stderr, "SigNozJsonWriter: SigNoz returned non-2xx status: %d. Response: %s\n", resp.StatusCode, string(bodyBytes))
	} else {
	}
}

func (sw *SigNozJsonWriter) Close() error {
	if sw.endpoint == "" || sw.accessToken == "" { // If writer was disabled
		return nil
	}
	fmt.Fprintln(os.Stdout, "SigNozJsonWriter: Close called, attempting to flush remaining logs...")
	close(sw.doneChan)
	sw.wg.Wait()
	fmt.Fprintln(os.Stdout, "SigNozJsonWriter: Closed.")
	return nil
}

func CloseLoggers() {
	log.Info().Msg("Closing loggers...")
	if globalSigNozWriter != nil {
		globalSigNozWriter.Close()
	}
	log.Info().Msg("Loggers closed.")
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

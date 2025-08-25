package logging

import (
	"context"
	"io"
	"os"
	"time"

	"github.com/cedana/cedana/internal/version"
	"github.com/cedana/cedana/pkg/config"
	"github.com/cedana/cedana/pkg/metrics"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/rs/zerolog/pkgerrors"
)

const (
	ZEROLOG_TIME_FORMAT_DEFAULT = time.RFC3339Nano // zerolog's default format for With().Timestamp()
	LOG_TIME_FORMAT             = time.TimeOnly
	LOG_CALLER_SKIP             = 3 // stack frame depth
)

var (
	Level              = zerolog.Disabled
	globalSigNozWriter *SigNozJsonWriter
)

type LineInfoHook struct{}

func (h LineInfoHook) Run(e *zerolog.Event, l zerolog.Level, msg string) {
	if l >= zerolog.ErrorLevel {
		e.Caller(LOG_CALLER_SKIP)
	}
}

func init() {
	InitLogger(config.Global.LogLevel)
}

func InitLogger(level string) {
	var err error
	zerolog.ErrorStackMarshaler = pkgerrors.MarshalStack

	Level, err := zerolog.ParseLevel(level)
	if err != nil || level == "" { // allow turning off logging
		Level = zerolog.Disabled
	}

	var consoleWriter io.Writer = zerolog.ConsoleWriter{
		Out:          os.Stdout,
		TimeFormat:   LOG_TIME_FORMAT,
		TimeLocation: time.Local,
	}

	var writers []io.Writer
	writers = append(writers, consoleWriter)

	if config.Global.Metrics {
		endpoint, headers, err := metrics.GetOtelCreds()
		if err != nil {
			log.Error().Err(err).Msg("failed to get otel creds")
		} else {
			host, err := utils.GetHost(context.Background())
			if err != nil {
				log.Error().Err(err).Msg("failed to get host info")
				return
			}
			clusterId, _ := os.LookupEnv("CEDANA_CLUSTER_ID")
			cedanaUrl := config.Global.Connection.URL
			version := version.GetVersion()

			resourceAttrs := map[string]string{
				"host.name":          host.Hostname,
				"cluster.id":         clusterId,
				"cedana.service.url": cedanaUrl,
				"version":            version,
			}

			// Add SigNoz writer
			globalSigNozWriter = NewSigNozJsonWriter(
				"https://"+endpoint+":443/logs/json",
				headers,
				"cedana",
				resourceAttrs,
				DEFAULT_MAX_BATCH_SIZE_JSON,
				DEFAULT_FLUSH_INTERVAL_MS_JSON,
			)

			writers = append(writers, globalSigNozWriter)
		}
	}
	multiWriter := io.MultiWriter(writers...)

	log.Logger = zerolog.New(multiWriter).
		Level(Level).
		With().
		Timestamp().
		Logger().Hook(LineInfoHook{})
}

func SetLogger(logger zerolog.Logger) {
	log.Logger = logger
}

func SetLevel(level string) {
	Level, err := zerolog.ParseLevel(level)
	if err != nil || level == "" { // allow turning off logging
		Level = zerolog.Disabled
	}
	log.Logger = log.Logger.Level(Level)
}

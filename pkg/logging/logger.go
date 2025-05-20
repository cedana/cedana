package logging

import (
	"io"
	"os"
	"time"

	"github.com/cedana/cedana/pkg/config"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/rs/zerolog/pkgerrors"
)

const (
	LOG_TIME_FORMAT = time.TimeOnly
	LOG_CALLER_SKIP = 3 // stack frame depth
)

var globalSigNozWriter *SigNozJsonWriter

type LineInfoHook struct{}

func (h LineInfoHook) Run(e *zerolog.Event, l zerolog.Level, msg string) {
	if l >= zerolog.ErrorLevel {
		e.Caller(LOG_CALLER_SKIP)
	}
}

func init() {
	SetLevel(config.Global.LogLevel)
}

func SetLevel(level string) {
	var err error
	zerolog.ErrorStackMarshaler = pkgerrors.MarshalStack

	logLevel, err := zerolog.ParseLevel(level)
	if err != nil || level == "" { // allow turning off logging
		logLevel = zerolog.Disabled
	}

	var consoleWriter io.Writer = zerolog.ConsoleWriter{
		Out:          os.Stdout,
		TimeFormat:   LOG_TIME_FORMAT,
		TimeLocation: time.Local,
	}

	if !config.Global.Metrics.Otel {
		log.Logger = zerolog.New(consoleWriter).
			Level(logLevel).
			With().
			Timestamp().
			Logger().Hook(LineInfoHook{})

		return
	} else {
		writers := []io.Writer{}
		writers = append(writers, consoleWriter)

		endpoint, headers, err := getOtelCreds()
		if err != nil {
			return
		}

		resourceAttrs := map[string]string{
			"host.name": os.Getenv("HOSTNAME"),
		}

		// Add SigNoz writer
		globalSigNozWriter = NewSigNozJsonWriter(
			"https://"+endpoint+"/logs/json",
			headers,
			"cedana",
			resourceAttrs,
			DEFAULT_MAX_BATCH_SIZE_JSON,
			DEFAULT_FLUSH_INTERVAL_MS_JSON,
		)

		writers = append(writers, globalSigNozWriter)
		multiWriter := io.MultiWriter(writers...)

		log.Logger = zerolog.New(multiWriter).
			Level(logLevel).
			With().
			Timestamp().
			Logger().Hook(LineInfoHook{})
	}
}

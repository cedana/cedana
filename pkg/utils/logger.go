package utils

import (
	"io"
	"os"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/pkgerrors"
)

const (
	DEFAULT_LOG_LEVEL    = zerolog.DebugLevel
	LOG_TIME_FORMAT_FULL = time.RFC3339
	LOG_CALLER_SKIP      = 3 // stack frame depth
)

var Logger zerolog.Logger

type LineInfoHook struct{}

func (h LineInfoHook) Run(e *zerolog.Event, l zerolog.Level, msg string) {
	if l >= zerolog.WarnLevel {
		e.Caller(LOG_CALLER_SKIP)
	}
}

func init() {
	zerolog.ErrorStackMarshaler = pkgerrors.MarshalStack

	logLevelStr := os.Getenv("CEDANA_LOG_LEVEL")
	logLevel, err := zerolog.ParseLevel(logLevelStr)
	if err != nil || logLevelStr == "" { // allow turning off logging
		logLevel = DEFAULT_LOG_LEVEL
	}

	var output io.Writer = zerolog.ConsoleWriter{
		Out: os.Stdout,
	}

	Logger = zerolog.New(output).
		Level(logLevel).
		With().
		Timestamp().
		Logger().Hook(LineInfoHook{})
}

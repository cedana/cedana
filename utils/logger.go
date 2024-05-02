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
)

var logger zerolog.Logger

func GetLogger() *zerolog.Logger {
	return &logger
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

	// If logging below debug level, also include caller info

	if logLevel <= zerolog.DebugLevel {
		logger = zerolog.New(output).
			Level(logLevel).
			With().
			Caller().
			Timestamp().
			Logger()
	} else {
		logger = zerolog.New(output).
			Level(logLevel).
			With().
			Timestamp().
			Logger()
	}
}

package utils

import (
	"io"
	"os"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/pkgerrors"
)

const defaultLogLevel = zerolog.DebugLevel

var logger zerolog.Logger

func GetLogger() *zerolog.Logger {
	return &logger
}

func init() {
	zerolog.ErrorStackMarshaler = pkgerrors.MarshalStack
	zerolog.TimeFieldFormat = time.RFC3339Nano

	logLevelStr := os.Getenv("CEDANA_LOG_LEVEL")
	logLevel, err := zerolog.ParseLevel(logLevelStr)
	if err != nil || logLevelStr == "" { // allow turning off logging
		logLevel = defaultLogLevel
	}

	var output io.Writer = zerolog.ConsoleWriter{
		Out:        os.Stdout,
		TimeFormat: time.RFC3339,
	}

	logger = zerolog.New(output).
		Level(logLevel).
		With().
		Timestamp().
		Logger()
}

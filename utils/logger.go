package utils

import (
	"io"
	"os"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/pkgerrors"
)

const DefaultLogLevel = zerolog.DebugLevel

var (
	once   sync.Once
	logger zerolog.Logger
)

func GetLogger() zerolog.Logger {
	once.Do(func() {
		zerolog.ErrorStackMarshaler = pkgerrors.MarshalStack
		zerolog.TimeFieldFormat = time.RFC3339Nano

		logLevelStr := os.Getenv("CEDANA_LOG_LEVEL")
		logLevel, err := zerolog.ParseLevel(logLevelStr)
		if err != nil || logLevelStr == "" { // allow turning off logging
			logLevel = DefaultLogLevel
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
	})

	return logger
}

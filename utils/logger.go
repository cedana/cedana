package utils

import (
	"io"
	"os"
	"runtime"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/pkgerrors"
)

const (
	DEFAULT_LOG_LEVEL      = zerolog.DebugLevel
	LOG_TIME_FORMAT_FULL   = time.RFC3339
	LOG_CALLER_STACK_DEPTH = 3 // XXX YA: Hack-y
)

var logger zerolog.Logger

func GetLogger() *zerolog.Logger {
	return &logger
}

type LineInfoHook struct{}

func (h LineInfoHook) Run(e *zerolog.Event, l zerolog.Level, msg string) {
	_, file, line, ok := runtime.Caller(LOG_CALLER_STACK_DEPTH)
	if ok && l >= zerolog.WarnLevel {
		e.Str("file", file)
		e.Int("line", line)
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

	logger = zerolog.New(output).
		Level(logLevel).
		With().
		Timestamp().
		Logger().Hook(LineInfoHook{})
}

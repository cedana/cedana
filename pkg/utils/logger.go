package utils

import (
	"context"
	"fmt"
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

type contextKey string

const loggerKey = contextKey("logger")

func WithLogger(ctx context.Context, logger *zerolog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey, logger)
}

func GetLoggerFromContext(ctx context.Context) (*zerolog.Logger, error) {
	logger, ok := ctx.Value(loggerKey).(*zerolog.Logger)
	if !ok {
		return &zerolog.Logger{}, fmt.Errorf("Logger not found in context")
	}
	return logger, nil
}

var logger zerolog.Logger

func GetLogger() *zerolog.Logger {
	return &logger
}

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

	logger = zerolog.New(output).
		Level(logLevel).
		With().
		Timestamp().
		Logger().Hook(LineInfoHook{})
}

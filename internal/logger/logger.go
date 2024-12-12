package logger

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
	DEFAULT_LOG_LEVEL    = zerolog.InfoLevel
	LOG_TIME_FORMAT_FULL = time.TimeOnly
	LOG_CALLER_SKIP      = 3 // stack frame depth
)

var DefaultLogger = log.Logger

type LineInfoHook struct{}

func (h LineInfoHook) Run(e *zerolog.Event, l zerolog.Level, msg string) {
	if l >= zerolog.WarnLevel {
		e.Caller(LOG_CALLER_SKIP)
	}
}

func init() {
	zerolog.ErrorStackMarshaler = pkgerrors.MarshalStack

	logLevelStr := config.Get(config.LOG_LEVEL)
	logLevel, err := zerolog.ParseLevel(logLevelStr)
	if err != nil || logLevelStr == "" { // allow turning off logging
		logLevel = DEFAULT_LOG_LEVEL
	}

	var output io.Writer = zerolog.ConsoleWriter{
		Out:          os.Stdout,
		TimeFormat:   LOG_TIME_FORMAT_FULL,
		TimeLocation: time.Local,
		PartsOrder:   []string{"time", "level", "caller", "message"},
		FieldsOrder:  []string{"time", "level", "caller", "message"},
	}

	// Set as default logger
	DefaultLogger = zerolog.New(output).
		Level(logLevel).
		With().
		Timestamp().
		Logger().Hook(LineInfoHook{})
}

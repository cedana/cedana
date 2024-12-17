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
	LOG_TIME_FORMAT = time.TimeOnly
	LOG_CALLER_SKIP = 3 // stack frame depth
)

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

	var output io.Writer = zerolog.ConsoleWriter{
		Out:          os.Stdout,
		TimeFormat:   LOG_TIME_FORMAT,
		TimeLocation: time.Local,
	}

	log.Logger = zerolog.New(output).
		Level(logLevel).
		With().
		Timestamp().
		Logger().Hook(LineInfoHook{})
}

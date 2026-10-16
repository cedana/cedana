package logging

import (
	"context"
	"io"
	"os"
	"time"

	"github.com/cedana/cedana/pkg/config"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/rs/zerolog/pkgerrors"
)

const (
	ZEROLOG_TIME_FORMAT_DEFAULT = time.RFC3339Nano // zerolog's default format for With().Timestamp()
	LOG_TIME_FORMAT             = time.TimeOnly
	LOG_CALLER_SKIP             = 3 // stack frame depth

	SERVICE_FIELD = "service.name" // Logs another service's lines as that service, e.g. cedana-gpu
)

var (
	Level         = zerolog.Disabled
	GlobalWriter  io.Writer
	ConsoleWriter = zerolog.ConsoleWriter{
		Out:          os.Stdout,
		TimeFormat:   LOG_TIME_FORMAT,
		TimeLocation: time.Local,
	}
)

type LineInfoHook struct{}

type hasCallerKey struct{}

// WithCaller marks a context whose events already carry their own caller,
// so LineInfoHook doesn't replace it with the daemon's.
func WithCaller(ctx context.Context) context.Context {
	return context.WithValue(ctx, hasCallerKey{}, true)
}

func (h LineInfoHook) Run(e *zerolog.Event, l zerolog.Level, msg string) {
	if l >= zerolog.ErrorLevel && e.GetCtx().Value(hasCallerKey{}) == nil {
		e.Caller(LOG_CALLER_SKIP)
	}
}

// Unfiltered returns the logger without the daemon's log level, for logs of programs
// with their own log level (CRIU, streamer, GPU). Stays off if logging is disabled.
func Unfiltered(logger zerolog.Logger) zerolog.Logger {
	if Level == zerolog.Disabled {
		return logger
	}
	return logger.Level(zerolog.TraceLevel)
}

func init() {
	zerolog.ErrorStackMarshaler = pkgerrors.MarshalStack
	Init(io.Discard)
}

func Init(writer io.Writer) {
	SetLevel(config.Global.LogLevel)
	GlobalWriter = writer
	log.Logger = zerolog.New(GlobalWriter).
		Level(Level).
		With().
		Timestamp().
		Logger().Hook(LineInfoHook{})
}

func Add(writer io.Writer) {
	if GlobalWriter == nil || GlobalWriter == io.Discard {
		Init(writer)
		return
	}
	log.Logger = zerolog.New(io.MultiWriter(GlobalWriter, writer)).
		Level(Level).
		With().
		Timestamp().
		Logger().Hook(LineInfoHook{})
}

func Get() *zerolog.Logger {
	return &log.Logger
}

func SetLevel(level string) {
	var err error
	Level, err = zerolog.ParseLevel(level)
	if err != nil || level == "" { // allow turning off logging
		Level = zerolog.Disabled
	}
	log.Logger = log.Logger.Level(Level)
}

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
	ZEROLOG_TIME_FORMAT_DEFAULT = time.RFC3339Nano // zerolog's default format for With().Timestamp()
	LOG_TIME_FORMAT             = time.TimeOnly
	LOG_CALLER_SKIP             = 3 // stack frame depth
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

func (h LineInfoHook) Run(e *zerolog.Event, l zerolog.Level, msg string) {
	if l >= zerolog.ErrorLevel {
		e.Caller(LOG_CALLER_SKIP)
	}
}

func init() {
	initLogger(config.Global.LogLevel, io.Discard)
}

func initLogger(level string, writers ...io.Writer) {
	var err error
	zerolog.ErrorStackMarshaler = pkgerrors.MarshalStack

	Level, err = zerolog.ParseLevel(level)
	if err != nil || level == "" { // allow turning off logging
		Level = zerolog.Disabled
	}

	for _, w := range writers {
		AddLogger(w)
	}
}

func AddLogger(writer io.Writer) {
	if GlobalWriter == nil || GlobalWriter == io.Discard {
		SetLogger(writer)
		return
	}
	log.Logger = zerolog.New(io.MultiWriter(GlobalWriter, writer)).
		Level(Level).
		With().
		Timestamp().
		Logger().Hook(LineInfoHook{})
}

func SetLogger(writer io.Writer) {
	GlobalWriter = writer
	log.Logger = zerolog.New(GlobalWriter).
		Level(Level).
		With().
		Timestamp().
		Logger().Hook(LineInfoHook{})
}

func GetLogger() zerolog.Logger {
	return log.Logger
}

func SetLevel(level string) {
	var err error
	Level, err = zerolog.ParseLevel(level)
	if err != nil || level == "" { // allow turning off logging
		Level = zerolog.Disabled
	}
	log.Logger = log.Logger.Level(Level)
}

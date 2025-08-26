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
	Level        = zerolog.Disabled
	GlobalWriter io.Writer
)

type LineInfoHook struct{}

func (h LineInfoHook) Run(e *zerolog.Event, l zerolog.Level, msg string) {
	if l >= zerolog.ErrorLevel {
		e.Caller(LOG_CALLER_SKIP)
	}
}

func init() {
	InitLogger(config.Global.LogLevel)
}

func InitLogger(level string) {
	var err error
	zerolog.ErrorStackMarshaler = pkgerrors.MarshalStack

	Level, err := zerolog.ParseLevel(level)
	if err != nil || level == "" { // allow turning off logging
		Level = zerolog.Disabled
	}

	GlobalWriter = zerolog.ConsoleWriter{
		Out:          os.Stdout,
		TimeFormat:   LOG_TIME_FORMAT,
		TimeLocation: time.Local,
	}

	log.Logger = zerolog.New(GlobalWriter).
		Level(Level).
		With().
		Timestamp().
		Logger().Hook(LineInfoHook{})
}

func AddLogger(writer io.Writer) {
	log.Logger = zerolog.New(zerolog.MultiLevelWriter(GlobalWriter, writer))
}

func SetLogger(writer io.Writer) {
	GlobalWriter = writer
	log.Logger = zerolog.New(GlobalWriter).
		Level(Level).
		With().
		Timestamp().
		Logger().Hook(LineInfoHook{})
}

func SetLevel(level string) {
	Level, err := zerolog.ParseLevel(level)
	if err != nil || level == "" { // allow turning off logging
		Level = zerolog.Disabled
	}
	log.Logger = log.Logger.Level(Level)
}

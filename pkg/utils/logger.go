package utils

import (
	"io"
	"os"
	"time"

	"github.com/natefinch/lumberjack"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/pkgerrors"
)

const (
	DEFAULT_LOG_LEVEL    = zerolog.InfoLevel
	LOG_TIME_FORMAT_FULL = time.RFC3339
	LOG_CALLER_SKIP      = 3 // stack frame depth
)

var Logger zerolog.Logger

type LineInfoHook struct{}

func (h LineInfoHook) Run(e *zerolog.Event, l zerolog.Level, msg string) {
	if l >= zerolog.WarnLevel {
		e.Caller(LOG_CALLER_SKIP)
	}
}

func init() {
	zerolog.ErrorStackMarshaler = pkgerrors.MarshalStack
	consoleWriter := zerolog.NewConsoleWriter(
		func(w *zerolog.ConsoleWriter) {
			w.Out = os.Stdout
		},
	)
	// "Writer" is just the the anonymous field "io.Writer" in the  struct
	consoleWriterLeveled := &LevelWriter{Writer: consoleWriter, Level: zerolog.DebugLevel}

	fileWriter := &lumberjack.Logger{
		Filename:   "/var/log/cedana-daemon-debug.log",
		MaxSize:    1,
		MaxAge:     30,
		MaxBackups: 5,
		LocalTime:  false,
		Compress:   false,
	}
	fileWriterLeveled := &LevelWriter{Writer: fileWriter, Level: zerolog.DebugLevel}

  mlw := zerolog.MultiLevelWriter(consoleWriterLeveled, fileWriterLeveled)

	Logger = zerolog.New(mlw).
		Level(zerolog.TraceLevel).
		With().
		Timestamp().
		Logger().Hook(LineInfoHook{})
}

type LevelWriter struct {
	io.Writer
	Level zerolog.Level
}

func (lw *LevelWriter) WriteLevel(l zerolog.Level, p []byte) (n int, err error) {
	if l >= lw.Level {
		return lw.Writer.Write(p)
	}
	return len(p), nil
}

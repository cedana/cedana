package logging

import (
	"io"
	"os"
	"time"

	"github.com/natefinch/lumberjack"
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

	// "Writer" is just the the anonymous field "io.Writer" in the  struct
	var output io.Writer = zerolog.ConsoleWriter{
		Out:          os.Stdout,
		TimeFormat:   LOG_TIME_FORMAT,
		TimeLocation: time.Local,
	}
	consoleWriterLeveled := &LevelWriter{Writer: output, Level: logLevel}

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

	log.Logger = zerolog.New(mlw).
		Level(logLevel).
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

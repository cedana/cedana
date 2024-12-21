package logging

// An io.Writer implementation that logs each line written to it.

import (
	"bytes"
	"io"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

type LogWriter struct {
	context string
	id      string
	logger  zerolog.Logger
	level   zerolog.Level

	io.Writer
}

func Writer(context string, id string, level zerolog.Level) *LogWriter {
	return &LogWriter{
		context: context,
		id:      id,
		logger:  log.Logger.Level(log.Logger.GetLevel()), // set minimum level to parent logger
		level:   level,
	}
}

func (w *LogWriter) Write(p []byte) (n int, err error) {
	lines := bytes.Split(p, []byte("\n"))
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		w.logger.WithLevel(w.level).Str("context", w.context).Str("id", w.id).Msg(string(line))
	}
	return len(p), nil
}

func (w *LogWriter) Close() error {
	return nil
}

package logging

// An io.Writer implementation that logs each line written to it.

import (
	"bytes"
	"context"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

type LogWriter struct {
	logger *zerolog.Logger
	level  zerolog.Level
}

func Writer(ctx context.Context, level zerolog.Level) *LogWriter {
	return &LogWriter{
		level:  level,
		logger: log.Ctx(ctx),
	}
}

func (w *LogWriter) Write(p []byte) (n int, err error) {
	lines := bytes.SplitSeq(p, []byte("\n"))
	for line := range lines {
		if len(line) == 0 {
			continue
		}
		w.logger.WithLevel(w.level).Msg(string(line))
	}

	return len(p), nil
}

func (w *LogWriter) Close() error {
	return nil
}

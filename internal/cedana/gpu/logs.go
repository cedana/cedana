package gpu

// Tails the log files written by the GPU controller, its workers and the interceptor/tracer
// in the workload, or reads the controller's stderr, and logs every line through the daemon's
// logger, so they reach the same writers (e.g. SigNoz) with the same context as the daemon's
// own logs.

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/cedana/cedana/pkg/logging"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

const (
	LOG_TAIL_INTERVAL = 500 * time.Millisecond
	GPU_SERVICE       = "cedana-gpu"
)

// Matches the spdlog pattern "[%Y-%m-%d %H:%M:%S.%f] [pid %P: tid %t] [%s:%!:%#] [%l] %v" of log files
var logLineRegex = regexp.MustCompile(`^\[[^\]]+\] \[pid (\d+): tid (\d+)\] \[(.*?)\] \[(trace|debug|info|warning|error|critical)\] (.*)$`)

// Matches the spdlog pattern "[%l] %v" the controller uses when logging to stderr
var stderrLineRegex = regexp.MustCompile(`^\[(trace|debug|info|warning|error|critical)\] (.*)$`)

var spdlogLevels = map[string]zerolog.Level{
	"trace":    zerolog.TraceLevel,
	"debug":    zerolog.DebugLevel,
	"info":     zerolog.InfoLevel,
	"warning":  zerolog.WarnLevel,
	"error":    zerolog.ErrorLevel,
	"critical": zerolog.FatalLevel, // WithLevel does not exit
}

// Logs GPU log lines with their own level, continuation lines with the level of the line before
type lineLogger struct {
	logger zerolog.Logger
	level  zerolog.Level // Of the last line, for continuation lines
}

type logFile struct {
	lineLogger
	file   *os.File
	reader *bufio.Reader
	rest   string // Incomplete last line
}

// Logs the controller's stderr written to it line by line
type logWriter struct {
	lineLogger
	rest []byte // Incomplete last line
}

// Returns the logger for GPU logs, which go out as their own service at their own level
func gpuLogger(id string) zerolog.Logger {
	return logging.Unfiltered(log.With().
		Str(logging.SERVICE_FIELD, GPU_SERVICE).
		Str("plugin", "gpu").
		Str("ID", id).
		Ctx(logging.WithCaller(context.Background())).
		Logger())
}

func newLogWriter(id string) *logWriter {
	return &logWriter{lineLogger: lineLogger{logger: gpuLogger(id), level: zerolog.InfoLevel}}
}

func (w *logWriter) Write(p []byte) (int, error) {
	w.rest = append(w.rest, p...)
	for {
		i := bytes.IndexByte(w.rest, '\n')
		if i < 0 {
			break
		}
		w.log(string(w.rest[:i]))
		w.rest = w.rest[i+1:]
	}
	return len(p), nil
}

// Tails all log files in dir until done is closed, then drains them one last time.
func tailLogs(dir string, id string, done <-chan int) {
	logger := gpuLogger(id)
	files := map[string]*logFile{}

	defer func() {
		for _, f := range files {
			f.file.Close()
		}
	}()

	for {
		finished := false
		select {
		case <-done:
			finished = true
		case <-time.After(LOG_TAIL_INTERVAL):
		}

		paths, _ := filepath.Glob(filepath.Join(dir, "*.log"))
		for _, path := range paths {
			f, ok := files[path]
			if !ok {
				file, err := os.Open(path)
				if err != nil {
					continue
				}
				f = &logFile{
					lineLogger: lineLogger{
						logger: logger.With().Str("component", logComponent(path)).Logger(),
						level:  zerolog.InfoLevel,
					},
					file:   file,
					reader: bufio.NewReader(file),
				}
				files[path] = f
			}
			f.tail()
		}

		if finished {
			return
		}
	}
}

// Logs all complete lines written since the last call
func (f *logFile) tail() {
	for {
		line, err := f.reader.ReadString('\n')
		if err == io.EOF {
			f.rest += line // Wait for the rest of the line to be written
			return
		}
		if err != nil {
			return
		}
		f.log(strings.TrimSuffix(f.rest+line, "\n"))
		f.rest = ""
	}
}

func (l *lineLogger) log(line string) {
	if matches := logLineRegex.FindStringSubmatch(line); matches != nil {
		l.level = spdlogLevels[matches[4]]
		l.logger.WithLevel(l.level).
			Str("PID", matches[1]).
			Str("TID", matches[2]).
			Str(zerolog.CallerFieldName, matches[3]).
			Msg(matches[5])
	} else if matches := stderrLineRegex.FindStringSubmatch(line); matches != nil {
		l.level = spdlogLevels[matches[1]]
		l.logger.WithLevel(l.level).Msg(matches[2])
	} else { // Continuation of a multi-line message
		l.logger.WithLevel(l.level).Msg(line)
	}
}

// Returns the component that wrote the log file, e.g. controller for cedana-gpu-controller-<pid>.log
func logComponent(path string) string {
	name := strings.TrimSuffix(filepath.Base(path), ".log")
	name = strings.TrimPrefix(name, "cedana-")
	if i := strings.LastIndex(name, "-"); i >= 0 {
		name = name[:i] // Drop the PID
	}
	switch name {
	case "so":
		return "interceptor"
	default:
		return strings.TrimPrefix(name, "gpu-")
	}
}

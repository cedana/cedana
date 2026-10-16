package gpu

// Tails the log files written by the GPU controller, its workers and the interceptor/tracer
// in the workload, and logs every line through the daemon's logger, so they reach the same
// writers (e.g. SigNoz) with the same context as the daemon's own logs.

import (
	"bufio"
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

const LOG_TAIL_INTERVAL = 500 * time.Millisecond

// Matches the spdlog pattern "[%Y-%m-%d %H:%M:%S.%f] [pid %P: tid %t] [%s:%!:%#] [%l] %v"
var logLineRegex = regexp.MustCompile(`^\[[^\]]+\] \[pid (\d+): tid (\d+)\] \[(.*?)\] \[(trace|debug|info|warning|error|critical)\] (.*)$`)

var spdlogLevels = map[string]zerolog.Level{
	"trace":    zerolog.TraceLevel,
	"debug":    zerolog.DebugLevel,
	"info":     zerolog.InfoLevel,
	"warning":  zerolog.WarnLevel,
	"error":    zerolog.ErrorLevel,
	"critical": zerolog.FatalLevel, // WithLevel does not exit
}

type logFile struct {
	file   *os.File
	reader *bufio.Reader
	logger zerolog.Logger
	level  zerolog.Level // Of the last line, for continuation lines
	rest   string        // Incomplete last line
}

// Tails all log files in dir until done is closed, then drains them one last time.
func tailLogs(dir string, id string, done <-chan int) {
	logger := log.With().Str("plugin", "gpu").Str("ID", id).Ctx(logging.WithCaller(context.Background())).Logger()
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
					file:   file,
					reader: bufio.NewReader(file),
					logger: logger.With().Str("component", logComponent(path)).Logger(),
					level:  zerolog.InfoLevel,
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
		line = strings.TrimSuffix(f.rest+line, "\n")
		f.rest = ""

		matches := logLineRegex.FindStringSubmatch(line)
		if matches == nil { // Continuation of a multi-line message
			f.logger.WithLevel(f.level).Msg(line)
			continue
		}

		f.level = spdlogLevels[matches[4]]
		f.logger.WithLevel(f.level).
			Str("PID", matches[1]).
			Str("TID", matches[2]).
			Str(zerolog.CallerFieldName, matches[3]).
			Msg(matches[5])
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

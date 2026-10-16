package criu

import (
	"bufio"
	"context"
	"os"
	"regexp"

	"github.com/cedana/cedana/pkg/logging"
	"github.com/rs/zerolog"
)

// CRIU log levels (criu/include/log.h)
const (
	CRIU_LOG_INFO = 3
)

// Matches the tag CRIU puts on errors and warnings, after the timestamp if any
var criuLogLevelRegex = regexp.MustCompile(`^(?:\(\s*[\d.]+\)\s+)?(Error|Warn)\s+\(`)

// Logs every line of a CRIU log file with its own level, whatever the daemon's log level.
// CRIU only tags errors and warnings. Other lines are info, or debug when CRIU logs at debug,
// as CRIU's info and debug lines look the same.
func logFromFile(logger zerolog.Logger, logfile string, criuLogLevel int32) {
	file, err := os.Open(logfile)
	if err != nil {
		return
	}
	defer file.Close()

	// Errors and warnings name their own source location
	logger = logging.Unfiltered(logger.With().Ctx(logging.WithCaller(context.Background())).Logger())

	untagged := zerolog.InfoLevel
	if criuLogLevel > CRIU_LOG_INFO {
		untagged = zerolog.DebugLevel
	}

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		level := untagged
		if matches := criuLogLevelRegex.FindStringSubmatch(line); matches != nil {
			switch matches[1] {
			case "Error":
				level = zerolog.ErrorLevel
			case "Warn":
				level = zerolog.WarnLevel
			}
		}
		logger.WithLevel(level).Msg(line)
	}
}

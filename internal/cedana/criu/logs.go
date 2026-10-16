package criu

import (
	"bufio"
	"context"
	"os"
	"regexp"

	"github.com/cedana/cedana/pkg/logging"
	"github.com/rs/zerolog"
)

const CRIU_SERVICE = "cedana-criu"

// Matches the tag CRIU puts on errors and warnings, after the timestamp if any
var criuLogLevelRegex = regexp.MustCompile(`^(?:\(\s*[\d.]+\)\s+)?(Error|Warn)\s+\(`)

// Logs every line of a CRIU log file, whatever the daemon's log level. CRIU only tags errors
// and warnings, which keep their level. Other lines are details to us, so debug.
func logFromFile(logger zerolog.Logger, logfile string) {
	file, err := os.Open(logfile)
	if err != nil {
		return
	}
	defer file.Close()

	// Errors and warnings name their own source location
	logger = logging.Unfiltered(logger.With().
		Str(logging.SERVICE_FIELD, CRIU_SERVICE).
		Ctx(logging.WithCaller(context.Background())).
		Logger())

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		level := zerolog.DebugLevel
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

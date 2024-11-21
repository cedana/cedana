package utils

// Defines all the log utility functions used by the server

import (
	"bufio"
	"context"
	"os"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// Log messages from a file, until the file exists
func LogFromFile(ctx context.Context, logfile string, level zerolog.Level) {
	log := log.Ctx(ctx)
	file, err := os.OpenFile(logfile, os.O_RDONLY, 0644)
	if err != nil {
		log.WithLevel(level).Str("file", logfile).Msg("failed to open log file")
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)

	// Read until ctx is done
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			log.WithLevel(level).Msg("context done")
			return
		default:
			log.WithLevel(level).Msg(scanner.Text())
		}
	}

	if err := scanner.Err(); err != nil {
		log.WithLevel(level).Err(err).Msg("finished reading log file")
	} else {
		log.WithLevel(level).Msg("finished reading log file")
	}
}

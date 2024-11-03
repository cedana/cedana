package utils

// Defines all the log utility functions used by the server

import (
	"bufio"
	"context"
	"os"

	"github.com/rs/zerolog/log"
)

// Log messages from a file, until the file exists
func ReadFileToLog(ctx context.Context, logfile string) {
	log := log.Ctx(ctx)
	file, err := os.OpenFile(logfile, os.O_RDONLY, 0644)
	if err != nil {
		log.Debug().Err(err).Msg("failed to open log file")
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)

	// Read until ctx is done
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			log.Debug().Msg("context done")
			return
		default:
			log.Debug().Msg(scanner.Text())
		}
	}

	if err := scanner.Err(); err != nil {
		log.Debug().Err(err).Msg("finished reading log file")
	} else {
		log.Debug().Msg("finished reading log file")
	}
}

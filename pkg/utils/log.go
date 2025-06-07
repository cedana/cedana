package utils

// Defines all the log utility functions used by the server

import (
	"bufio"
	"context"
	"os"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

const waitTime = 200 * time.Millisecond

// Log messages from a file.
// Can provide an arbitrary format function to format the log message.
// Noop if the current log level is higher than the provided level
func LogFromFile(ctx context.Context, logfile string, level zerolog.Level, format ...func([]byte) (string, error)) (lastMsg string) {
	if log.Logger.GetLevel() > level {
		return
	}

	log := log.Ctx(ctx)
	var file *os.File

	file, err := os.Open(logfile)
	if err != nil {
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)

	// Read until ctx is done
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
			if len(format) > 0 {
				bytes := scanner.Bytes()
				lastMsg, err = format[0](bytes)
				if err != nil {
					log.WithLevel(level).Err(err).Msg("failed to format log message")
					break
				}
			} else {
				lastMsg = scanner.Text()
			}

			log.WithLevel(level).Msg(lastMsg)
		}
	}

	return
}

func LastMsgFromFile(logfile string, format ...func([]byte) (string, error)) (lastMsg string, err error) {
	file, err := os.Open(logfile)
	if err != nil {
		return "", err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		if len(format) > 0 {
			bytes := scanner.Bytes()
			lastMsg, err = format[0](bytes)
			if err != nil {
				return "", err
			}
		} else {
			lastMsg = scanner.Text()
		}
	}

	if err := scanner.Err(); err != nil {
		return "", err
	}

	return lastMsg, nil
}

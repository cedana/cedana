package logging

// Defines all the log utility functions used by the server

import (
	"bufio"
	"context"
	"os"
	"os/user"
	"path/filepath"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// Log messages from a file.
// Can provide an arbitrary format function to format the log message.
// Noop if the current log level is higher than the provided level
func FromFile(ctx context.Context, logfile string, level zerolog.Level, format ...func([]byte) (string, error)) (lastMsg string) {
	if log.Logger.GetLevel() > level {
		return lastMsg
	}

	log := log.Ctx(ctx)
	var file *os.File

	file, err := os.Open(logfile)
	if err != nil {
		return lastMsg
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)

	// Read until ctx is done
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return lastMsg
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

	return lastMsg
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

// LogFile returns the path to a log file based on program name and whether the program is running as root or not.
// If the program is running as root, it will return /var/log/cedana-<program>.log, otherwise it will return
// $HOME/.cedana/logs/cedana-<program>.log
func LogFile(program string) string {
	if os.Geteuid() == 0 {
		return filepath.Join("/var/log", "cedana-"+program+".log")
	}
	user, err := user.Current()
	if err != nil {
		os.MkdirAll(filepath.Join(os.TempDir(), "cedana", "logs"), 0o755)
		return filepath.Join(os.TempDir(), "cedana", "logs", "cedana-"+program+".log")
	}
	os.MkdirAll(filepath.Join(user.HomeDir, ".cedana", "logs"), 0o755)
	return filepath.Join(user.HomeDir, ".cedana", "logs", "cedana-"+program+".log")
}

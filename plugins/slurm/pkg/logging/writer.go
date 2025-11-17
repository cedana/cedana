package logging

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/cedana/cedana/pkg/logging"
	"github.com/rs/zerolog"
)

const TIME_FORMAT = time.RFC3339 // Runc's default time format

type Line struct {
	Msg   string `json:"msg"`
	Level string `json:"level"`
	Time  string `json:"time"`
}

type LogWriter struct {
	format string
	writer io.Writer
}

func Writer(w io.Writer, format string) *LogWriter {
	return &LogWriter{
		format: format,
		writer: w,
	}
}

func (lw *LogWriter) Write(p []byte) (int, error) {
	var zerologEntry map[string]any
	if err := json.Unmarshal(p, &zerologEntry); err != nil {
		return len(p), nil // Consume and drop
	}

	// Timestamp
	var ts int64 = time.Now().UnixNano() // Default to now
	if tsStr, ok := zerologEntry[zerolog.TimestampFieldName].(string); ok {
		parsedTime, err := time.Parse(logging.ZEROLOG_TIME_FORMAT_DEFAULT, tsStr)
		if err == nil {
			ts = parsedTime.UnixNano()
		}
	}
	tsFormatted := time.Unix(0, ts).Format(TIME_FORMAT)

	levelStr, _ := zerologEntry[zerolog.LevelFieldName].(string)
	parsedLevel, _ := zerolog.ParseLevel(levelStr) // Handles error by defaulting to NoLevel
	body, _ := zerologEntry[zerolog.MessageFieldName].(string)
	error, _ := zerologEntry[zerolog.ErrorFieldName].(string)
	if error != "" {
		body = fmt.Sprintf("%s: %s", body, error)
	}

	var written int
	switch strings.ToUpper(lw.format) {
	case "JSON":
		line := Line{
			Msg:   body,
			Level: parsedLevel.String(),
			Time:  tsFormatted,
		}
		data, err := json.Marshal(line)
		if err != nil {
			return written, err
		}
		n, err := lw.writer.Write(append(data, '\n'))
		written += n
		if err != nil {
			return written, err
		}
	default:
		logLine := fmt.Sprintf("time=\"%s\" level=\"%s\" msg=\"%s\"\n", tsFormatted, parsedLevel.String(), body)
		n, err := lw.writer.Write([]byte(logLine))
		written += n
		if err != nil {
			return written, err
		}
	}

	return len(p), nil
}

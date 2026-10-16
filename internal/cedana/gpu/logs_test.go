package gpu

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cedana/cedana/pkg/logging"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// Lines are logged with their own level and source, continuation lines with the level of
// the line they continue, and a line still being written only once it is complete.
func TestTailLogs(t *testing.T) {
	var out bytes.Buffer
	defer func(l zerolog.Logger) { log.Logger = l }(log.Logger)
	log.Logger = zerolog.New(&out).Level(zerolog.InfoLevel).Hook(logging.LineInfoHook{})

	dir := t.TempDir()
	path := filepath.Join(dir, "cedana-so-1234.log")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	file.WriteString("[2026-10-15 10:00:00.000001] [pid 1234: tid 1235] [driver.cpp:wcuInit:42] [debug] dropped\n")
	file.WriteString("[2026-10-15 10:00:00.000002] [pid 1234: tid 1235] [driver.cpp:wcuInit:43] [warning] first\n")
	file.WriteString("continued\n")
	file.WriteString("[2026-10-15 10:00:00.000003] [pid 1234: tid 1236] [api.cpp:operator[]:7] [error] sec")

	done := make(chan int)
	finished := make(chan struct{})
	go func() {
		tailLogs(dir, "abc", done)
		close(finished)
	}()

	time.Sleep(2 * LOG_TAIL_INTERVAL)
	file.WriteString("ond\n")
	close(done)
	<-finished

	var lines []map[string]string
	for line := range strings.SplitSeq(strings.TrimSpace(out.String()), "\n") {
		var entry map[string]string
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("bad log line %q: %v", line, err)
		}
		lines = append(lines, entry)
	}

	want := []map[string]string{
		{"level": "warn", "message": "first", "PID": "1234", "TID": "1235", "caller": "driver.cpp:wcuInit:43"},
		{"level": "warn", "message": "continued"},
		{"level": "error", "message": "second", "PID": "1234", "TID": "1236", "caller": "api.cpp:operator[]:7"},
	}
	if len(lines) != len(want) {
		t.Fatalf("got %d lines, want %d: %v", len(lines), len(want), lines)
	}
	for i, w := range want {
		w["plugin"] = "gpu"
		w["ID"] = "abc"
		w["component"] = "interceptor"
		for k, v := range w {
			if lines[i][k] != v {
				t.Errorf("line %d: %s = %q, want %q", i, k, lines[i][k], v)
			}
		}
	}
}

func TestLogComponent(t *testing.T) {
	for path, want := range map[string]string{
		"/tmp/cedana-gpu.x/cedana-gpu-controller-12.log": "controller",
		"/tmp/cedana-gpu.x/cedana-gpu-worker-34.log":     "worker",
		"/tmp/cedana-gpu.x/cedana-so-56.log":             "interceptor",
		"/tmp/cedana-gpu.x/cedana-tracer-78.log":         "tracer",
	} {
		if got := logComponent(path); got != want {
			t.Errorf("logComponent(%q) = %q, want %q", path, got, want)
		}
	}
}

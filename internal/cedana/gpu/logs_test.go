package gpu

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/cedana/cedana/pkg/logging"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// Lines are logged with their own level and source, whatever the daemon's level but never at
// trace, continuation
// lines with the level of the line they continue, and a line still being written only once it
// is complete.
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

	file.WriteString("[2026-10-15 10:00:00.000000] [pid 1234: tid 1235] [driver.cpp:wcuInit:41] [trace] dropped\n")
	file.WriteString("[2026-10-15 10:00:00.000001] [pid 1234: tid 1235] [driver.cpp:wcuInit:42] [debug] zeroth\n")
	file.WriteString("[2026-10-15 10:00:00.000002] [pid 1234: tid 1235] [driver.cpp:wcuInit:43] [warning] first\n")
	file.WriteString("continued\n")
	file.WriteString("[2026-10-15 10:00:00.000003] [pid 1234: tid 1236] [api.cpp:operator[]:7] [error] sec")

	done := make(chan int)
	finished := make(chan struct{})
	go func() {
		tailLogs(dir, "abc", done, false)
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
		{"level": "debug", "message": "zeroth", "PID": "1234", "TID": "1235", "caller": "driver.cpp:wcuInit:42"},
		{"level": "warn", "message": "first", "PID": "1234", "TID": "1235", "caller": "driver.cpp:wcuInit:43"},
		{"level": "warn", "message": "continued"},
		{"level": "error", "message": "second", "PID": "1234", "TID": "1236", "caller": "api.cpp:operator[]:7"},
	}
	if len(lines) != len(want) {
		t.Fatalf("got %d lines, want %d: %v", len(lines), len(want), lines)
	}
	for i, w := range want {
		w["service.name"] = GPU_SERVICE
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

// The controller's stderr is logged the same way, in whatever chunks it is written
func TestLogWriter(t *testing.T) {
	var out bytes.Buffer
	defer func(l zerolog.Logger) { log.Logger = l }(log.Logger)
	log.Logger = zerolog.New(&out).Level(zerolog.InfoLevel).Hook(logging.LineInfoHook{})

	w := newLogWriter("abc")
	w.Write([]byte("[debug] zeroth\n[warning] fir"))
	w.Write([]byte("st\ncontinued\n[error] second\n"))

	var lines []map[string]string
	for line := range strings.SplitSeq(strings.TrimSpace(out.String()), "\n") {
		var entry map[string]string
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("bad log line %q: %v", line, err)
		}
		lines = append(lines, entry)
	}

	want := []map[string]string{
		{"level": "debug", "message": "zeroth"},
		{"level": "warn", "message": "first"},
		{"level": "warn", "message": "continued"},
		{"level": "error", "message": "second", "caller": ""},
	}
	if len(lines) != len(want) {
		t.Fatalf("got %d lines, want %d: %v", len(lines), len(want), lines)
	}
	for i, w := range want {
		w["service.name"] = GPU_SERVICE
		w["plugin"] = "gpu"
		w["ID"] = "abc"
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

// A buffer the tailer can log to while the test reads it
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// Runs the tailer on dir while write is called, and returns the GPU log messages it logged.
// write is given the messages logged so far.
func tailMessages(t *testing.T, dir string, resume bool, write func(logged []string)) []string {
	var out syncBuffer
	defer func(l zerolog.Logger) { log.Logger = l }(log.Logger)
	log.Logger = zerolog.New(&out).Level(zerolog.InfoLevel)

	done := make(chan int)
	finished := make(chan struct{})
	go func() {
		tailLogs(dir, "abc", done, resume)
		close(finished)
	}()
	time.Sleep(2 * LOG_TAIL_INTERVAL)
	write(gpuMessages(t, out.String()))
	time.Sleep(2 * LOG_TAIL_INTERVAL)
	close(done)
	<-finished

	return gpuMessages(t, out.String())
}

// Returns the messages of the GPU log lines in out
func gpuMessages(t *testing.T, out string) []string {
	var messages []string
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("bad log line %q: %v", line, err)
		}
		if entry["service.name"] == GPU_SERVICE {
			messages = append(messages, entry["message"].(string))
		}
	}
	return messages
}

// The workload owns its log directory, so links and special files in it, or a link in its
// place, must not make us log files it can't read
func TestTailLogsUnsafeFiles(t *testing.T) {
	root := t.TempDir()
	secret := filepath.Join(root, "secret")
	os.WriteFile(secret, []byte("[2026-10-15 10:00:00.000001] [pid 1: tid 1] [a.cpp:f:1] [info] secret\n"), 0o600)

	dir := filepath.Join(root, "logs")
	os.Mkdir(dir, 0o755)
	os.Symlink(secret, filepath.Join(dir, "cedana-so-1.log"))
	if err := syscall.Mkfifo(filepath.Join(dir, "cedana-so-2.log"), 0o644); err != nil {
		t.Fatal(err)
	}
	os.Symlink(root, filepath.Join(root, "link")) // A directory link in place of the log directory
	os.WriteFile(filepath.Join(root, "cedana-so-3.log"), []byte("[2026-10-15 10:00:00.000001] [pid 1: tid 1] [a.cpp:f:1] [info] linked\n"), 0o644)

	messages := tailMessages(t, dir, false, func([]string) {
		os.WriteFile(filepath.Join(dir, "cedana-so-4.log"), []byte("[2026-10-15 10:00:00.000001] [pid 1: tid 1] [a.cpp:f:1] [info] safe\n"), 0o644)
	})
	if strings.Join(messages, ",") != "safe" {
		t.Errorf("got messages %v, want only [safe]", messages)
	}

	if messages := tailMessages(t, filepath.Join(root, "link"), false, func([]string) {}); len(messages) != 0 {
		t.Errorf("followed a link to the log directory, got messages %v", messages)
	}
}

// When resuming, what files already hold was logged by an earlier daemon, but files
// that show up later are logged in full
func TestTailLogsResume(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cedana-gpu-controller-1.log")
	os.WriteFile(path, []byte("[2026-10-15 10:00:00.000001] [pid 1: tid 1] [a.cpp:f:1] [info] old\n"), 0o644)

	messages := tailMessages(t, dir, true, func([]string) {
		file, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
		file.WriteString("[2026-10-15 10:00:00.000002] [pid 1: tid 1] [a.cpp:f:2] [info] appended\n")
		file.Close()
		os.WriteFile(filepath.Join(dir, "cedana-gpu-worker-2.log"), []byte("[2026-10-15 10:00:00.000003] [pid 2: tid 2] [a.cpp:f:3] [info] new file\n"), 0o644)
	})
	slices.Sort(messages) // Files are read in directory order
	if strings.Join(messages, ",") != "appended,new file" {
		t.Errorf("got messages %v, want [appended new file]", messages)
	}
}

// A line longer than MAX_LOG_LINE_SIZE is cut and the rest of it dropped, whether it comes
// from a log file or stderr, so a line without an end can't use up our memory
func TestLogLineSize(t *testing.T) {
	long := strings.Repeat("x", 3*MAX_LOG_LINE_SIZE)
	check := func(t *testing.T, messages []string) {
		if len(messages) != 2 || len(messages[0]) > MAX_LOG_LINE_SIZE+len(" (truncated)") || messages[1] != "after" {
			t.Errorf("got %d messages, first of length %d, want a cut line then [after]", len(messages), len(messages[0]))
		}
	}

	t.Run("file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "cedana-so-1.log")
		os.WriteFile(path, []byte(long), 0o644) // Not ended yet
		check(t, tailMessages(t, dir, false, func([]string) {
			file, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
			file.WriteString(long + "\n[2026-10-15 10:00:00.000001] [pid 1: tid 1] [a.cpp:f:1] [info] after\n")
			file.Close()
		}))
	})

	t.Run("stderr", func(t *testing.T) {
		var out bytes.Buffer
		defer func(l zerolog.Logger) { log.Logger = l }(log.Logger)
		log.Logger = zerolog.New(&out).Level(zerolog.InfoLevel)

		w := newLogWriter("abc")
		w.Write([]byte(long))
		w.Write([]byte(long + "\n[info] after\n"))
		if len(w.rest) != 0 {
			t.Errorf("kept %d bytes of a dropped line", len(w.rest))
		}

		var messages []string
		for line := range strings.SplitSeq(strings.TrimSpace(out.String()), "\n") {
			var entry map[string]string
			json.Unmarshal([]byte(line), &entry)
			messages = append(messages, entry["message"])
		}
		check(t, messages)
	})
}

// At most MAX_LOG_FILES are tailed at once, and files that are removed are let go
func TestLogFileLimit(t *testing.T) {
	dir := t.TempDir()
	line := func(i int) []byte {
		return fmt.Appendf(nil, "[2026-10-15 10:00:00.000001] [pid 1: tid 1] [a.cpp:f:1] [info] %d\n", i)
	}
	for i := range MAX_LOG_FILES + 10 {
		os.WriteFile(filepath.Join(dir, fmt.Sprintf("cedana-so-%d.log", i)), line(i), 0o644)
	}

	messages := tailMessages(t, dir, false, func(logged []string) {
		if len(logged) != MAX_LOG_FILES {
			t.Errorf("tailed %d files, want %d", len(logged), MAX_LOG_FILES)
		}
		for _, i := range logged[:10] { // Make room for the ones left out
			os.Remove(filepath.Join(dir, fmt.Sprintf("cedana-so-%s.log", i)))
		}
	})
	if len(messages) != MAX_LOG_FILES+10 {
		t.Errorf("got %d messages, want %d, one per file once there is room", len(messages), MAX_LOG_FILES+10)
	}
}

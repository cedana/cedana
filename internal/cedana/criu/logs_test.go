package criu

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

// Errors and warnings keep their level whatever the daemon's level, and other lines are
// info, or debug when CRIU logs at debug.
func TestLogFromFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), CRIU_DUMP_LOG_FILE)
	err := os.WriteFile(path, []byte(
		"(00.000010) Error (criu/cr-dump.c:2121): Dumping FAILED.\n"+
			"(00.000020) Warn  (criu/kerndat.c:89): Can't load /run/criu.kdat\n"+
			"(00.000030) Dumping path for 5 fd via self 7 [/]\n",
	), 0o644)
	if err != nil {
		t.Fatal(err)
	}

	for criuLogLevel, untagged := range map[int32]string{2: "info", 3: "info", 4: "debug"} {
		var out bytes.Buffer
		logFromFile(zerolog.New(&out).Level(zerolog.ErrorLevel), path, criuLogLevel)

		var levels []string
		for line := range strings.SplitSeq(strings.TrimSpace(out.String()), "\n") {
			var entry map[string]string
			if err := json.Unmarshal([]byte(line), &entry); err != nil {
				t.Fatalf("bad log line %q: %v", line, err)
			}
			levels = append(levels, entry["level"])
		}

		want := []string{"error", "warn", untagged}
		if strings.Join(levels, ",") != strings.Join(want, ",") {
			t.Errorf("CRIU log level %d: got levels %v, want %v", criuLogLevel, levels, want)
		}
	}
}

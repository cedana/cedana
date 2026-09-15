package gpu

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFailedReplays(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		want  []string
	}{
		{
			name: "every task restored",
			files: map[string]string{
				"cedana-replay-entry-11":  "deadbeef 1000\n",
				"cedana-replay-entry-12":  "deadbeef 1000\n",
				"cedana-replay-status-11": "ok\n",
				"cedana-replay-status-12": "ok\n",
			},
			want: nil,
		},
		{
			name: "one task failed its replay",
			files: map[string]string{
				"cedana-replay-entry-11":  "deadbeef 1000\n",
				"cedana-replay-entry-12":  "deadbeef 1000\n",
				"cedana-replay-status-11": "ok\n",
				"cedana-replay-status-12": "err failed to open file gpu-ctxshm-0-12\n",
			},
			want: []string{"task 12: err failed to open file"},
		},
		{
			name: "one task's replay never ran",
			files: map[string]string{
				"cedana-replay-entry-11":  "deadbeef 1000\n",
				"cedana-replay-entry-12":  "deadbeef 1000\n",
				"cedana-replay-status-11": "ok\n",
			},
			want: []string{"task 12: its replay left no status"},
		},
		{
			name: "no replay ran at all",
			files: map[string]string{
				"cedana-replay-entry-11": "deadbeef 1000\n",
				"cedana-replay-entry-12": "deadbeef 1000\n",
			},
			want: []string{"task 11: its replay left no status", "task 12: its replay left no status"},
		},
		{
			name: "a job that never used a GPU",
			files: map[string]string{
				"core-11.img": "x",
			},
			want: nil,
		},
		{
			name: "gpu images with no replay entries",
			files: map[string]string{
				"core-11.img":   "x",
				"gpu-calls-0-0": "x",
				"gpu-mem-0-0":   "x",
			},
			want: []string{"no replay entries"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for name, body := range tt.files {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
					t.Fatalf("writing %s: %v", name, err)
				}
			}

			got := failedReplays(dir)
			if len(got) != len(tt.want) {
				t.Fatalf("got %d failures %v, want %d", len(got), got, len(tt.want))
			}
			for _, want := range tt.want {
				found := false
				for _, g := range got {
					if strings.Contains(g, want) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("no reported failure contains %q; got %v", want, got)
				}
			}
		})
	}
}

func TestFailedReplaysUnreadableDirectory(t *testing.T) {
	if got := failedReplays(filepath.Join(t.TempDir(), "does-not-exist")); len(got) != 1 {
		t.Fatalf("expected one failure for an unreadable directory, got %v", got)
	}
}

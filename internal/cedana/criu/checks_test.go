package criu

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCheckInotify(t *testing.T) {
	tests := []struct {
		name     string
		content  *string
		data     string
		warnings int
	}{
		{name: "default is low", content: ptr("128\n"), data: "128", warnings: 1},
		{name: "raised", content: ptr("8192\n"), data: "8192", warnings: 0},
		{name: "higher than recommended", content: ptr("65536"), data: "65536", warnings: 0},
		{name: "unparseable", content: ptr("lots"), data: "unknown", warnings: 1},
		{name: "missing", content: nil, data: "unknown", warnings: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "max_user_instances")
			if tt.content != nil {
				if err := os.WriteFile(path, []byte(*tt.content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			c := checkInotify(path)
			if c.Data != tt.data {
				t.Errorf("data = %q, want %q", c.Data, tt.data)
			}
			if len(c.Warnings) != tt.warnings {
				t.Errorf("warnings = %v, want %d", c.Warnings, tt.warnings)
			}
			if len(c.Errors) != 0 {
				t.Errorf("errors = %v, want none", c.Errors)
			}
		})
	}
}

func ptr(s string) *string { return &s }

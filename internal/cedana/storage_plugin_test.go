package cedana

import "testing"

func TestStoragePluginName(t *testing.T) {
	for path, want := range map[string]string{
		"s3://bucket/key":        "storage/s3",
		"gs://bucket/key":        "storage/gcs",
		"gcs://bucket/key":       "storage/gcs",
		"cedana://checkpoint":    "storage/cedana",
		"csx://checkpoint/thing": "storage/csx",
	} {
		if got := storagePluginName(path); got != want {
			t.Errorf("storagePluginName(%q) = %q, want %q", path, got, want)
		}
	}
}

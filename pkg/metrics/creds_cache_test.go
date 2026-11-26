package metrics

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func withCredsCache(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cache", "otel-credentials.json")
	old := credsCachePath
	credsCachePath = path
	t.Cleanup(func() { credsCachePath = old })
	return path
}

func TestCredsCacheRoundTrip(t *testing.T) {
	path := withCredsCache(t)
	want := &Creds{Endpoint: "ingest.example:443", Headers: "key"}

	if got := loadCachedCreds("https://u", "tok"); got != nil {
		t.Fatalf("empty cache returned %+v", got)
	}
	storeCachedCreds("https://u", "tok", want)

	got := loadCachedCreds("https://u", "tok")
	if got == nil || *got != *want {
		t.Fatalf("loadCachedCreds = %+v, want %+v", got, want)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("cache file mode = %o, want 600", perm)
	}
}

// Credentials fetched with another URL or token, or too long ago, are not used
func TestCredsCacheMisses(t *testing.T) {
	path := withCredsCache(t)
	storeCachedCreds("https://u", "tok", &Creds{Endpoint: "e:443", Headers: "h"})

	if loadCachedCreds("https://u", "other") != nil {
		t.Error("cache hit for a different token")
	}
	if loadCachedCreds("https://other", "tok") != nil {
		t.Error("cache hit for a different URL")
	}

	stale, _ := json.Marshal(cachedCreds{
		Key:       credsCacheKey("https://u", "tok"),
		FetchedAt: time.Now().Add(-2 * CREDENTIALS_CACHE_TTL),
		Creds:     Creds{Endpoint: "e:443", Headers: "h"},
	})
	if err := os.WriteFile(path, stale, 0o600); err != nil {
		t.Fatal(err)
	}
	if loadCachedCreds("https://u", "tok") != nil {
		t.Error("cache hit for stale credentials")
	}

	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if loadCachedCreds("https://u", "tok") != nil {
		t.Error("cache hit for a corrupt file")
	}
}

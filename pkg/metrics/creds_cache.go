package metrics

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// The credentials are fetched from the propagator over HTTPS, which costs a few hundred
// milliseconds. Every container a Kubernetes node starts through Cedana runs the shim and
// the CLI, each initializing telemetry before it can do any work, so the fetch would sit
// on the critical path of every container start. They are cached on the node instead.

const (
	CREDENTIALS_CACHE_DIR = "/run/cedana"
	CREDENTIALS_CACHE_TTL = 1 * time.Hour
)

var credsCachePath = filepath.Join(CREDENTIALS_CACHE_DIR, "otel-credentials.json")

type cachedCreds struct {
	Key       string    `json:"key"` // Of the URL and auth token the credentials were fetched with
	FetchedAt time.Time `json:"fetched_at"`
	Creds
}

func credsCacheKey(url, authToken string) string {
	sum := sha256.Sum256([]byte(url + "\x00" + authToken))
	return hex.EncodeToString(sum[:])
}

// loadCachedCreds returns the cached credentials for url and authToken, or nil when there
// are none, they are stale, or the cache can't be read.
func loadCachedCreds(url, authToken string) *Creds {
	data, err := os.ReadFile(credsCachePath)
	if err != nil {
		return nil
	}
	var cached cachedCreds
	if err := json.Unmarshal(data, &cached); err != nil {
		return nil
	}
	if cached.Key != credsCacheKey(url, authToken) || time.Since(cached.FetchedAt) > CREDENTIALS_CACHE_TTL {
		return nil
	}
	if cached.Endpoint == "" || cached.Headers == "" {
		return nil
	}
	return &cached.Creds
}

// storeCachedCreds writes creds for url and authToken, readable only by the owner. Best
// effort: a node where the cache can't be written fetches on every start, as before.
func storeCachedCreds(url, authToken string, creds *Creds) {
	data, err := json.Marshal(cachedCreds{Key: credsCacheKey(url, authToken), FetchedAt: time.Now(), Creds: *creds})
	if err != nil {
		return
	}
	dir := filepath.Dir(credsCachePath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	tmp, err := os.CreateTemp(dir, ".otel-credentials-*")
	if err != nil {
		return
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return
	}
	if err := tmp.Close(); err != nil {
		return
	}
	os.Rename(tmp.Name(), credsCachePath) // Atomic: concurrent readers see the old or the new file
}

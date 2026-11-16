package metrics

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/cedana/cedana/pkg/config"
)

// Every one-shot CLI run (each container restore) would otherwise block on the credentials fetch.
const (
	CREDENTIALS_CACHE_FILE = "otel-credentials.json"
	CREDENTIALS_CACHE_TTL  = time.Hour
)

type cachedCreds struct {
	Creds
	Key     string    `json:"key"`
	Fetched time.Time `json:"fetched"`
}

func credsCachePath() string {
	if config.Dir == "" {
		return ""
	}
	return filepath.Join(config.Dir, CREDENTIALS_CACHE_FILE)
}

// credsCacheKey ties cached credentials to the endpoint and token they were fetched with.
func credsCacheKey(url, authToken string) string {
	sum := sha256.Sum256([]byte(url + "\x00" + authToken))
	return hex.EncodeToString(sum[:16])
}

func loadCachedCreds(key string) *Creds {
	path := credsCachePath()
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var c cachedCreds
	if json.Unmarshal(data, &c) != nil || c.Key != key || time.Since(c.Fetched) > CREDENTIALS_CACHE_TTL ||
		c.Endpoint == "" || c.Headers == "" {
		return nil
	}
	return &c.Creds
}

// storeCachedCreds is best effort: a read-only config dir only costs the fetch next time.
func storeCachedCreds(key string, creds *Creds) {
	path := credsCachePath()
	if path == "" {
		return
	}
	data, err := json.Marshal(cachedCreds{Creds: *creds, Key: key, Fetched: time.Now()})
	if err != nil {
		return
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), CREDENTIALS_CACHE_FILE+".*")
	if err != nil {
		return
	}
	defer os.Remove(tmp.Name())
	if _, err = tmp.Write(data); err != nil {
		tmp.Close()
		return
	}
	if tmp.Close() != nil {
		return
	}
	os.Rename(tmp.Name(), path)
}

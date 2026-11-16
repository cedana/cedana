package plugins

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/cedana/cedana/pkg/config"
)

// Exec'ing every plugin binary with --version costs ~0.3 s per CLI run (each container restore),
// so versions are cached by the binary's path, size and modification time.
const VERSION_CACHE_FILE = "plugin-versions.json"

var versionCache struct {
	sync.Mutex
	loaded  bool
	entries map[string]string
}

func versionCachePath() string {
	if config.Dir == "" {
		return ""
	}
	return filepath.Join(config.Dir, VERSION_CACHE_FILE)
}

func versionCacheKey(path string) string {
	s, err := os.Stat(path)
	if err != nil || s.IsDir() {
		return ""
	}
	return fmt.Sprintf("%s:%d:%d", path, s.Size(), s.ModTime().UnixNano())
}

func loadVersionCache() {
	if versionCache.loaded {
		return
	}
	versionCache.loaded = true
	versionCache.entries = make(map[string]string)
	if path := versionCachePath(); path != "" {
		if data, err := os.ReadFile(path); err == nil {
			json.Unmarshal(data, &versionCache.entries)
		}
	}
}

// cachedBinaryVersion returns the cached --version output for the binary at path.
func cachedBinaryVersion(path string) (string, bool) {
	key := versionCacheKey(path)
	if key == "" {
		return "", false
	}
	versionCache.Lock()
	defer versionCache.Unlock()
	loadVersionCache()
	v, ok := versionCache.entries[key]
	return v, ok
}

// storeBinaryVersion is best effort: a read-only config dir only costs the exec next time.
func storeBinaryVersion(path, version string) {
	key := versionCacheKey(path)
	if key == "" {
		return
	}
	versionCache.Lock()
	defer versionCache.Unlock()
	loadVersionCache()
	if versionCache.entries[key] == version {
		return
	}
	for k := range versionCache.entries { // drop entries of an older build of the same binary
		if len(k) > len(path) && k[:len(path)+1] == path+":" {
			delete(versionCache.entries, k)
		}
	}
	versionCache.entries[key] = version
	file := versionCachePath()
	if file == "" {
		return
	}
	data, err := json.Marshal(versionCache.entries)
	if err != nil {
		return
	}
	tmp, err := os.CreateTemp(filepath.Dir(file), VERSION_CACHE_FILE+".*")
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
	os.Chmod(tmp.Name(), 0o644)
	os.Rename(tmp.Name(), file)
}

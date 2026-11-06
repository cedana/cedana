package plugins

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// Getting a plugin syncs its install state into the copy returned, never into the global
// Registry: concurrent dumps each get a plugin, and writing into the shared Registry from
// all of them was a data race (found by the race-detector CI job)
func TestLocalManagerGetLeavesRegistryAlone(t *testing.T) {
	originalBinDir := BinDir
	t.Cleanup(func() { BinDir = originalBinDir })
	BinDir = t.TempDir()
	if err := os.WriteFile(filepath.Join(BinDir, "criu"), []byte("#!/bin/sh\necho 'Version: 4.2'\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	registryChecksum := func() string {
		for _, p := range Registry {
			if p.Name == "criu" {
				return p.Binaries[0].Checksum
			}
		}
		t.Fatal("no criu in the registry")
		return ""
	}
	before := registryChecksum()

	m := NewLocalManager()
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 10 {
				if p := m.Get("criu"); p == nil || p.Binaries[0].Checksum == "" {
					t.Error("Get didn't sync the plugin it returned")
				}
				m.IsInstalled("criu")
				if _, err := m.List(false, "criu"); err != nil {
					t.Error(err)
				}
			}
		})
	}
	wg.Wait()

	if after := registryChecksum(); after != before {
		t.Errorf("the global Registry was modified: criu's checksum %q -> %q", before, after)
	}
}

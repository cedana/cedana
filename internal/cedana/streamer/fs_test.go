package streamer

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cedana/cedana/internal/cedana/filesystem"
	"github.com/spf13/afero"
)

func TestGlob(t *testing.T) {
	streamerBinary := "/usr/local/bin/cedana-image-streamer"
	if _, err := os.Stat(streamerBinary); os.IsNotExist(err) {
		t.Skipf("streamer binary not found at %s, skipping integration test", streamerBinary)
	}

	tmpDir := t.TempDir()
	captureDir := filepath.Join(tmpDir, "capture")
	serveDir := filepath.Join(tmpDir, "serve")
	shardDir := filepath.Join(tmpDir, "shards")

	for _, dir := range []string{captureDir, serveDir, shardDir} {
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatalf("failed to create dir %s: %v", dir, err)
		}
	}

	ctx := context.Background()
	storage := &filesystem.Storage{}
	streams := int32(2)

	t.Log("Phase 1: Capture - writing files to streamer")
	dumpFs, waitDump, err := NewStreamingFs(
		ctx,
		streamerBinary,
		captureDir,
		storage,
		shardDir,
		streams,
		WRITE_ONLY,
		"none",
	)
	if err != nil {
		t.Fatalf("failed to create streaming fs for dump: %v", err)
	}

	testFiles := map[string]string{
		"gpu-hostmem-0":    "hostmem data 0",
		"gpu-hostmem-1":    "hostmem data 1",
		"gpu-hostmem-2":    "hostmem data 2",
		"other-file.img":   "other data",
		"gpu-checkpoint-0": "checkpoint 0",
	}

	for filename, content := range testFiles {
		file, err := dumpFs.Create(filename)
		if err != nil {
			t.Fatalf("failed to create file %s: %v", filename, err)
		}
		if _, err := file.Write([]byte(content)); err != nil {
			file.Close()
			t.Fatalf("failed to write file %s: %v", filename, err)
		}
		if err := file.Close(); err != nil {
			t.Fatalf("failed to close file %s: %v", filename, err)
		}
	}

	if err := waitDump(); err != nil {
		t.Fatalf("dump wait failed: %v", err)
	}

	shardFiles, err := os.ReadDir(shardDir)
	if err != nil {
		t.Fatalf("failed to read shard dir: %v", err)
	}
	t.Logf("Created %d shard files", len(shardFiles))
	totalBytes := 0
	for _, f := range shardFiles {
		info, _ := f.Info()
		totalBytes += int(info.Size())
		t.Logf("  - %s (%d bytes)", f.Name(), info.Size())
	}
	t.Logf("Total shard data: %d bytes", totalBytes)

	if totalBytes == 0 {
		t.Fatal("No data written to shards! Dump may have failed.")
	}

	t.Log("Phase 2: Serve - reading files from shards and testing afero.Glob")
	restoreFs, waitRestore, err := NewStreamingFs(
		ctx,
		streamerBinary,
		serveDir,
		storage,
		shardDir,
		streams,
		READ_ONLY,
	)
	if err != nil {
		t.Fatalf("failed to create streaming fs for restore: %v", err)
	}
	defer waitRestore()

	sockPath := filepath.Join(serveDir, "streamer-serve.sock")
	for i := range 50 {
		if _, err := os.Stat(sockPath); err == nil {
			t.Logf("Streamer socket ready after %d ms", i*10)
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if _, err := os.Stat(sockPath); err != nil {
		t.Fatalf("Streamer socket never appeared: %v", err)
	}

	t.Log("Testing afero.Glob patterns...")

	tests := []struct {
		pattern       string
		description   string
		expectedFiles []string
	}{
		{
			pattern:       "*",
			description:   "all files",
			expectedFiles: []string{"gpu-hostmem-0", "gpu-hostmem-1", "gpu-hostmem-2", "other-file.img", "gpu-checkpoint-0"},
		},
		{
			pattern:       "gpu-hostmem-*",
			description:   "gpu-hostmem files",
			expectedFiles: []string{"gpu-hostmem-0", "gpu-hostmem-1", "gpu-hostmem-2"},
		},
		{
			pattern:       "*.img",
			description:   "img files",
			expectedFiles: []string{"other-file.img"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			t.Logf("Testing afero.Glob pattern: %s", tt.pattern)
			matches, err := afero.Glob(restoreFs, tt.pattern)
			if err != nil {
				t.Fatalf("afero.Glob(%q) failed: %v", tt.pattern, err)
			}

			t.Logf("Got matches: %v", matches)

			if len(matches) == 0 && len(tt.expectedFiles) > 0 {
				t.Errorf("afero.Glob(%q) returned empty results, expected %d files: %v",
					tt.pattern, len(tt.expectedFiles), tt.expectedFiles)
				return
			}

			if len(matches) != len(tt.expectedFiles) {
				t.Errorf("afero.Glob(%q) = %d matches, want %d. Got: %v",
					tt.pattern, len(matches), len(tt.expectedFiles), matches)
			}

			matchMap := make(map[string]bool)
			for _, match := range matches {
				matchMap[match] = true
			}

			for _, expected := range tt.expectedFiles {
				if !matchMap[expected] {
					t.Errorf("afero.Glob(%q) missing expected file %q. Got: %v",
						tt.pattern, expected, matches)
				}
			}
		})
	}

	t.Log("Testing file content retrieval for all matched files...")
	allMatches, err := restoreFs.glob("*")
	if err != nil {
		t.Fatalf("Failed to glob all files: %v", err)
	}

	for _, filename := range allMatches {
		file, err := restoreFs.Open(filename)
		if err != nil {
			t.Errorf("failed to open %s: %v", filename, err)
			continue
		}

		expectedContent := testFiles[filename]
		buf := make([]byte, len(expectedContent))
		n, err := file.Read(buf)
		file.Close()

		if err != nil {
			t.Errorf("failed to read %s: %v", filename, err)
			continue
		}

		if n != len(expectedContent) {
			t.Errorf("%s: read %d bytes, expected %d", filename, n, len(expectedContent))
			continue
		}

		if string(buf) != expectedContent {
			t.Errorf("%s: content = %q, expected %q", filename, string(buf), expectedContent)
		} else {
			t.Logf("%s: content verified", filename)
		}
	}
}

package streamer

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cedana/cedana/internal/cedana/filesystem"
)

// TestGlobIntegration tests glob pattern matching in streaming fs.
func TestGlobIntegration(t *testing.T) {
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

	// create testfiles simulating a dump
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

	// wait for io to finish
	if err := waitDump(); err != nil {
		t.Fatalf("dump wait failed: %v", err)
	}

	// dump files sharded from streaming
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

	// this is for simulating restore and reading from streaming shards
	t.Log("Serve - reading files from shards and testing Glob")
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
		// here we wait for streamer socket to be ready
		if _, err := os.Stat(sockPath); err == nil {
			t.Logf("Streamer socket ready after %d ms", i*10)
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if _, err := os.Stat(sockPath); err != nil {
		t.Fatalf("Streamer socket never appeared: %v", err)
	}

	// time.Sleep(100 * time.Millisecond)

	t.Log("Testing glob patterns...")

	allFiles, err := restoreFs.Glob("*")
	if err != nil {
		t.Fatalf("Glob('*') failed: %v", err)
	}
	t.Logf("All files (*): %v", allFiles)

	gpuHostmemMatches, err := restoreFs.Glob("gpu-hostmem-*")
	if err != nil {
		t.Fatalf("Glob('gpu-hostmem-*') failed: %v", err)
	}
	t.Logf("gpu-hostmem-* matches: %v", gpuHostmemMatches)

	if len(gpuHostmemMatches) != 3 {
		t.Errorf("Expected 3 gpu-hostmem-* matches, got %d: %v", len(gpuHostmemMatches), gpuHostmemMatches)
	}

	imgMatches, err := restoreFs.Glob("*.img")
	if err != nil {
		t.Fatalf("Glob('*.img') failed: %v", err)
	}
	t.Logf("*.img matches: %v", imgMatches)

	if len(imgMatches) != 1 {
		t.Errorf("Expected 1 *.img match, got %d: %v", len(imgMatches), imgMatches)
	}

	checkpointMatches, err := restoreFs.Glob("gpu-checkpoint-*")
	if err != nil {
		t.Fatalf("Glob('gpu-checkpoint-*') failed: %v", err)
	}
	t.Logf("gpu-checkpoint-* matches: %v", checkpointMatches)

	if len(checkpointMatches) != 1 {
		t.Errorf("Expected 1 gpu-checkpoint-* match, got %d: %v", len(checkpointMatches), checkpointMatches)
	}

	if len(allFiles) != len(testFiles) {
		t.Errorf("Expected %d total files, got %d: %v", len(testFiles), len(allFiles), allFiles)
	}

	t.Log("Testing file content retrieval via Glob...")
	for _, filename := range gpuHostmemMatches {
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

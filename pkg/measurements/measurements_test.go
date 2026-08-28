//go:build linux

package measurements

import (
	"context"
	"errors"
	"os"
	"syscall"
	"testing"
)

func TestMatchStorageUsesLongestMount(t *testing.T) {
	storage := []StorageMeasurement{
		{Name: "/"},
		{Name: "/mnt"},
		{Name: "/mnt/checkpoints"},
	}

	got := matchStorage("/mnt/checkpoints/job-1", storage)
	if got == nil || got.Name != "/mnt/checkpoints" {
		t.Fatalf("matchStorage = %#v, want /mnt/checkpoints", got)
	}
}

func TestParseNUMAMemTotalGB(t *testing.T) {
	meminfo := "Node 0 MemTotal:       12345678 kB\nNode 0 MemFree:         1234 kB\n"
	got := parseNUMAMemTotalGB(meminfo)
	want := float64(12345678*1024) / 1_000_000_000
	if got != want {
		t.Fatalf("parseNUMAMemTotalGB = %f, want %f", got, want)
	}
}

func TestMeasurementFailureCodes(t *testing.T) {
	tests := []struct {
		err  error
		code string
	}{
		{syscall.EACCES, FailurePermissionDenied},
		{syscall.EROFS, FailureReadOnly},
		{syscall.ENOSPC, FailureInsufficientSpace},
		{errDataCorruption, FailureDataCorruption},
		{errNUMABinding, FailureNUMABinding},
		{errInvalidOutput, FailureInvalidOutput},
		{errors.New("device failed"), FailureIO},
	}

	for _, test := range tests {
		if got := measurementFailure("test", test.err); got == nil || got.Code != test.code {
			t.Fatalf("measurementFailure(%v) = %#v, want code %q", test.err, got, test.code)
		}
	}
}

func TestBenchmarkStorageSeparatesCachedAndColdModes(t *testing.T) {
	results, err := BenchmarkStorage(
		context.Background(),
		t.TempDir(),
		0.01,
		2,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("results = %d, want 2", len(results))
	}

	for i, mode := range []string{StorageModeCached, StorageModeCold} {
		result := results[i]
		if result.Mode != mode {
			t.Fatalf("result[%d].Mode = %q, want %q", i, result.Mode, mode)
		}
		if result.BenchmarkSizeGB != 0.01 || result.BenchmarkRuns != 2 {
			t.Fatalf("result[%d] context = size:%f runs:%d", i, result.BenchmarkSizeGB, result.BenchmarkRuns)
		}
		if result.CapacityGB <= 0 {
			t.Fatalf("result[%d] missing filesystem capacity", i)
		}
		if result.ReadFailure != nil || result.WriteFailure != nil {
			t.Fatalf("result[%d] failures = read:%v write:%v", i, result.ReadFailure, result.WriteFailure)
		}
		if result.ReadGBPerSec == nil || *result.ReadGBPerSec <= 0 {
			t.Fatalf("result[%d] missing read rate", i)
		}
		if result.WriteGBPerSec == nil || *result.WriteGBPerSec <= 0 {
			t.Fatalf("result[%d] missing write rate", i)
		}
	}
}

func TestBenchmarkStorageResolvesRelativePathForMountMatching(t *testing.T) {
	previousDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previousDir) })
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}

	results, err := BenchmarkStorage(context.Background(), ".", 0.001, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("results = %d, want 2", len(results))
	}
	for _, result := range results {
		if result.Name == "." {
			t.Fatal("relative benchmark path was not matched to its mount")
		}
	}
}

func TestBenchmarkMemoryBacksPagesAndRecordsSamples(t *testing.T) {
	measurement, err := BenchmarkMemory(context.Background(), 0.01, 2)
	if err != nil {
		t.Fatal(err)
	}
	if measurement.CopyGBPerSec == nil || *measurement.CopyGBPerSec <= 0 {
		t.Fatal("missing memory copy rate")
	}
	if measurement.Placement != "os_default" {
		t.Fatalf("placement = %q, want os_default", measurement.Placement)
	}
	if measurement.BenchmarkSizeGB != 0.01 || measurement.BenchmarkRuns != 2 {
		t.Fatalf("context = size:%f runs:%d", measurement.BenchmarkSizeGB, measurement.BenchmarkRuns)
	}
}

func TestBenchmarkMemoryRejectsInvalidSampleCount(t *testing.T) {
	if _, err := BenchmarkMemory(context.Background(), 0.01, 0); err == nil {
		t.Fatal("expected invalid sample count error")
	}
}

func TestBenchmarkReadDetectsCorruption(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "corrupt-*")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	pattern := []byte("expected")
	if _, err := file.Write([]byte("corrupt!")); err != nil {
		t.Fatal(err)
	}
	if _, err := benchmarkRead(context.Background(), file, int64(len(pattern)), pattern); err != errDataCorruption {
		t.Fatalf("error = %v, want errDataCorruption", err)
	}
}

func TestBenchmarkStorageReturnsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := BenchmarkStorage(ctx, t.TempDir(), 0.001, 1)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

package measurements

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	memoryBenchmarkRuns  = 3
	storageBenchmarkRuns = 3
)

const (
	StorageModeCached = "cached"
	StorageModeCold   = "cold"
)

func ParseStorageModes(value string) ([]string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "both":
		return []string{StorageModeCached, StorageModeCold}, nil
	case StorageModeCached:
		return []string{StorageModeCached}, nil
	case StorageModeCold:
		return []string{StorageModeCold}, nil
	default:
		return nil, fmt.Errorf("invalid storage mode %q: use cached, cold, or both", value)
	}
}

func BenchmarkStorage(ctx context.Context, path string, sizeGB float64, modes []string) ([]StorageMeasurement, error) {
	if path == "" {
		path = "."
	}
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	path = absolutePath
	if len(modes) == 0 {
		modes = []string{StorageModeCached, StorageModeCold}
	}
	if sizeGB <= 0 {
		sizeGB = 1
	}

	storage, err := CollectStorage(ctx)
	if err != nil {
		return nil, err
	}
	pattern := make([]byte, benchmarkChunkSize)
	if _, err := rand.Read(pattern); err != nil {
		return nil, err
	}
	results := make([]StorageMeasurement, 0, len(modes))
	for _, mode := range modes {
		if err := ctx.Err(); err != nil {
			return results, err
		}
		if mode != StorageModeCached && mode != StorageModeCold {
			return nil, fmt.Errorf("unsupported storage benchmark mode %q", mode)
		}
		result := benchmarkStorageMode(ctx, path, sizeGB, mode, storage, pattern)
		results = append(results, result)
		if err := ctx.Err(); err != nil {
			return results, err
		}
	}
	return results, nil
}

func benchmarkStorageMode(ctx context.Context, path string, sizeGB float64, mode string, storage []StorageMeasurement, pattern []byte) StorageMeasurement {
	var result StorageMeasurement
	readRates := make([]float64, 0, storageBenchmarkRuns)
	writeRates := make([]float64, 0, storageBenchmarkRuns)
	for run := range storageBenchmarkRuns {
		result = benchmarkStorageAt(ctx, path, sizeGB, mode, storage, pattern)
		result.BenchmarkRuns = run + 1
		if result.ReadFailure != nil || result.WriteFailure != nil {
			return result
		}
		readRates = append(readRates, *result.ReadGBPerSec)
		writeRates = append(writeRates, *result.WriteGBPerSec)
	}
	sort.Float64s(readRates)
	sort.Float64s(writeRates)
	result.ReadGBPerSec = &readRates[len(readRates)/2]
	result.WriteGBPerSec = &writeRates[len(writeRates)/2]
	return result
}
func benchmarkStorageAt(ctx context.Context, path string, sizeGB float64, mode string, storage []StorageMeasurement, pattern []byte) StorageMeasurement {
	mount := filepath.Clean(path)
	backend := "unknown"
	capacityGB := float64(0)
	if matched := matchStorage(path, storage); matched != nil {
		mount = matched.Name
		backend = matched.Backend
		capacityGB = matched.CapacityGB
	}
	result := StorageMeasurement{
		Name:            mount,
		Backend:         backend,
		CapacityGB:      capacityGB,
		Mode:            mode,
		BenchmarkSizeGB: sizeGB,
		BenchmarkRuns:   1,
		ReadSource:      SourceUnknown,
		ReadConfidence:  ConfidenceNone,
		WriteSource:     SourceUnknown,
		WriteConfidence: ConfidenceNone,
	}
	if mode == StorageModeCached {
		result.CachePolicy = "page_cache"
	} else {
		result.CachePolicy = "fadvise_dontneed"
	}

	file, err := os.CreateTemp(path, "cedana-measurement-*")
	if err != nil {
		failure := measurementFailure("storage_create", err)
		result.ReadFailure = failure
		result.WriteFailure = failure
		return result
	}
	name := file.Name()
	defer os.Remove(name)
	defer file.Close()

	total := int64(sizeGB * 1_000_000_000)

	writeRate, err := benchmarkWrite(ctx, file, total, pattern, mode == StorageModeCold)
	if err != nil {
		result.WriteFailure = measurementFailure("storage_write_"+mode, err)
		result.ReadFailure = &Failure{Code: FailureNotMeasured, Operation: "storage_read_" + mode, Message: "read skipped because the benchmark write failed"}
		return result
	}
	result.WriteGBPerSec = &writeRate
	result.WriteSource = SourceMeasured
	result.WriteConfidence = ConfidenceMedium

	if mode == StorageModeCold {
		if err := evictFileCache(file); err != nil {
			result.ReadFailure = cacheEvictionFailure("storage_cache_evict", err)
			return result
		}
	}

	readRate, err := benchmarkRead(ctx, file, total, pattern)
	if err != nil {
		result.ReadFailure = measurementFailure("storage_read_"+mode, err)
		return result
	}
	result.ReadGBPerSec = &readRate
	result.ReadSource = SourceMeasured
	result.ReadConfidence = ConfidenceMedium
	return result
}

func BenchmarkMemory(ctx context.Context, sizeGB float64) (*MemoryMeasurement, error) {
	rate, sizeGB, err := benchmarkMemoryCopy(ctx, sizeGB)
	if err != nil {
		return nil, err
	}

	memory, err := collectMemory(ctx)
	if memory == nil {
		memory = &MemoryMeasurement{}
	}
	memory.BenchmarkSizeGB = sizeGB
	memory.BenchmarkRuns = memoryBenchmarkRuns
	memory.Placement = "os_default"
	memory.CopyGBPerSec = &rate
	memory.Source = SourceMeasured
	memory.Confidence = ConfidenceMedium
	if nodes, numaErr := CollectNUMA(ctx); numaErr == nil && len(nodes) > 1 {
		memory.Confidence = ConfidenceLow
	}
	memory.Failure = nil
	return memory, err
}

func BenchmarkMemoryRate(ctx context.Context, sizeGB float64) (float64, error) {
	rate, _, err := benchmarkMemoryCopy(ctx, sizeGB)
	return rate, err
}

func benchmarkMemoryCopy(ctx context.Context, sizeGB float64) (float64, float64, error) {
	if sizeGB <= 0 {
		sizeGB = 1
	}
	if err := ctx.Err(); err != nil {
		return 0, 0, err
	}

	size := int(sizeGB * 1_000_000_000)
	if size <= 0 {
		return 0, 0, fmt.Errorf("benchmark size is too small")
	}
	sizeGB = float64(size) / 1_000_000_000
	src := make([]byte, size)
	dst := make([]byte, size)
	touchMemoryPages(src, 0x5a)
	touchMemoryPages(dst, 0xa5)

	rates := make([]float64, 0, memoryBenchmarkRuns)
	for range memoryBenchmarkRuns {
		if err := ctx.Err(); err != nil {
			return 0, 0, err
		}
		start := time.Now()
		if copied := copy(dst, src); copied != size {
			return 0, 0, fmt.Errorf("copied %d of %d bytes", copied, size)
		}
		duration := time.Since(start)
		if duration <= 0 {
			return 0, 0, fmt.Errorf("memory benchmark duration is zero")
		}
		rates = append(rates, sizeGB/duration.Seconds())
	}
	if !memoryCopyMatches(src, dst) {
		return 0, 0, errDataCorruption
	}
	sort.Float64s(rates)
	return rates[len(rates)/2], sizeGB, nil
}
func touchMemoryPages(buffer []byte, seed byte) {
	pageSize := os.Getpagesize()
	for offset := 0; offset < len(buffer); offset += pageSize {
		buffer[offset] = seed + byte(offset/pageSize)
	}
	buffer[len(buffer)-1] = seed ^ 0xff
}

func memoryCopyMatches(src, dst []byte) bool {
	if len(src) != len(dst) || len(src) == 0 {
		return false
	}
	pageSize := os.Getpagesize()
	for offset := 0; offset < len(src); offset += pageSize {
		if src[offset] != dst[offset] {
			return false
		}
	}
	return src[len(src)-1] == dst[len(dst)-1]
}
func benchmarkWrite(ctx context.Context, file *os.File, total int64, pattern []byte, durable bool) (float64, error) {
	if err := file.Truncate(0); err != nil {
		return 0, err
	}
	if _, err := file.Seek(0, 0); err != nil {
		return 0, err
	}

	start := time.Now()
	remaining := total
	for remaining > 0 {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		n := int64(len(pattern))
		if remaining < n {
			n = remaining
		}
		if _, err := file.Write(pattern[:int(n)]); err != nil {
			return 0, err
		}
		remaining -= n
	}
	if durable {
		if err := file.Sync(); err != nil {
			return 0, err
		}
	}
	duration := time.Since(start)
	return float64(total) / 1_000_000_000 / duration.Seconds(), nil
}

func benchmarkRead(ctx context.Context, file *os.File, total int64, pattern []byte) (float64, error) {
	if _, err := file.Seek(0, 0); err != nil {
		return 0, err
	}
	buffer := make([]byte, len(pattern))
	start := time.Now()
	remaining := total
	for remaining > 0 {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		n := int64(len(buffer))
		if remaining < n {
			n = remaining
		}
		if _, err := io.ReadFull(file, buffer[:int(n)]); err != nil {
			return 0, err
		}
		if !bytes.Equal(buffer[:int(n)], pattern[:int(n)]) {
			return 0, errDataCorruption
		}
		remaining -= n
	}
	duration := time.Since(start)
	return float64(total) / 1_000_000_000 / duration.Seconds(), nil
}

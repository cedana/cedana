package measurements

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	mem "github.com/shirou/gopsutil/v4/mem"
)

const (
	SourceConfigured = "configured"
	SourceMeasured   = "measured"
	SourceUnknown    = "unknown"

	DefaultStorageCalibrationEntries = 64
	DefaultStorageCalibrationTTL     = 30 * time.Minute
)

type Report struct {
	Storage []StorageMeasurement
	Memory  *MemoryMeasurement
	NUMA    []NUMAMeasurement
}

type StorageMeasurement struct {
	Name            string
	CapacityGB      float64
	Mode            string
	BenchmarkSizeGB float64
	BenchmarkRuns   int
	ReadGBPerSec    *float64
	ReadSource      string
	ReadFailure     *Failure
	WriteGBPerSec   *float64
	WriteSource     string
	WriteFailure    *Failure
}

type StorageCalibration struct {
	Resource        string
	ReadThroughput  int64
	WriteThroughput int64
}

type StorageCalibrator struct {
	mu      sync.Mutex
	entries map[string]*storageCalibrationEntry
	max     int
	ttl     time.Duration
}

type storageCalibrationEntry struct {
	calibration *StorageCalibration
	err         error
	expiresAt   time.Time
	ready       chan struct{}
}

func NewStorageCalibrator(maxEntries int, ttl time.Duration) *StorageCalibrator {
	if maxEntries <= 0 {
		maxEntries = DefaultStorageCalibrationEntries
	}
	if ttl <= 0 {
		ttl = DefaultStorageCalibrationTTL
	}
	return &StorageCalibrator{
		entries: make(map[string]*storageCalibrationEntry),
		max:     maxEntries,
		ttl:     ttl,
	}
}

func (c *StorageCalibrator) Calibrate(ctx context.Context, path string) (*StorageCalibration, error) {
	resource, err := storageResource(ctx, path)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	c.evictExpired(time.Now())
	if entry := c.entries[resource]; entry != nil {
		ready := entry.ready
		c.mu.Unlock()
		select {
		case <-ready:
			return entry.calibration, entry.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if !c.evictOne() {
		c.mu.Unlock()
		return nil, fmt.Errorf("storage calibration cache is full")
	}
	entry := &storageCalibrationEntry{ready: make(chan struct{})}
	c.entries[resource] = entry
	c.mu.Unlock()

	calibration, err := CalibrateStorage(ctx, path)

	c.mu.Lock()
	entry.calibration = calibration
	entry.err = err
	entry.expiresAt = time.Now().Add(c.ttl)
	close(entry.ready)
	c.mu.Unlock()
	return calibration, err
}

func (c *StorageCalibrator) Lookup(ctx context.Context, path string) (*StorageCalibration, bool, error) {
	resource, err := storageResource(ctx, path)
	if err != nil {
		return nil, false, err
	}

	c.mu.Lock()
	c.evictExpired(time.Now())
	entry := c.entries[resource]
	if entry == nil {
		c.mu.Unlock()
		return nil, false, nil
	}
	ready := entry.ready
	c.mu.Unlock()

	select {
	case <-ready:
		if entry.err != nil || entry.calibration == nil {
			return nil, false, entry.err
		}
		return entry.calibration, true, nil
	case <-ctx.Done():
		return nil, false, ctx.Err()
	}
}

func (c *StorageCalibrator) evictExpired(now time.Time) {
	for resource, entry := range c.entries {
		if !entry.expiresAt.IsZero() && !entry.expiresAt.After(now) {
			delete(c.entries, resource)
		}
	}
}

func (c *StorageCalibrator) evictOne() bool {
	if len(c.entries) < c.max {
		return true
	}
	for resource, entry := range c.entries {
		select {
		case <-entry.ready:
			delete(c.entries, resource)
			return true
		default:
		}
	}
	return false
}

func storageResource(ctx context.Context, path string) (string, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if info, statErr := os.Stat(path); statErr == nil && !info.IsDir() {
		path = filepath.Dir(path)
	}
	storage, err := CollectStorage(ctx)
	if err != nil {
		return "", err
	}
	if matched := matchStorage(path, storage); matched != nil {
		return matched.Name, nil
	}
	return path, nil
}

type MemoryMeasurement struct {
	HostID          string
	Hostname        string
	TotalGB         float64
	Placement       string
	BenchmarkSizeGB float64
	BenchmarkRuns   int
	CopyGBPerSec    *float64
	Source          string
	Failure         *Failure
}

type NUMAMeasurement struct {
	Name            string
	Kind            string
	Locality        string
	MemoryGB        float64
	BenchmarkSizeGB float64
	BenchmarkRuns   int
	CopyGBPerSec    *float64
	Source          string
	Failure         *Failure
}

const (
	FailureNotMeasured       = "not_measured"
	FailurePermissionDenied  = "permission_denied"
	FailureReadOnly          = "read_only"
	FailureInsufficientSpace = "insufficient_space"
	FailureNotFound          = "not_found"
	FailureUnsupported       = "unsupported"
	FailureUnavailable       = "unavailable"
	FailureCacheEviction     = "cache_eviction_failed"
	FailureNUMABinding       = "numa_binding_failed"
	FailureDataCorruption    = "data_corruption"
	FailureCancelled         = "cancelled"
	FailureTimeout           = "timeout"
	FailureInvalidOutput     = "invalid_output"
	FailureIO                = "io_error"
)

var (
	errCacheEvictionUnsupported = errors.New("cache eviction is unsupported on this platform")
	errNUMABinding              = errors.New("NUMA memory binding failed")
	errDataCorruption           = errors.New("benchmark data did not match what was written")
	errInvalidOutput            = errors.New("benchmark returned invalid output")
)

type Failure struct {
	Code      string `json:"code"`
	Operation string `json:"operation,omitempty"`
	Message   string `json:"message"`
}

func notMeasuredFailure(operation, hint string) *Failure {
	return &Failure{Code: FailureNotMeasured, Operation: operation, Message: hint}
}

func measurementFailure(operation string, err error) *Failure {
	if err == nil {
		return nil
	}
	code := FailureIO
	switch {
	case errors.Is(err, context.Canceled):
		code = FailureCancelled
	case errors.Is(err, context.DeadlineExceeded):
		code = FailureTimeout
	case errors.Is(err, errCacheEvictionUnsupported):
		code = FailureUnsupported
	case errors.Is(err, errNUMABinding):
		code = FailureNUMABinding
	case errors.Is(err, errDataCorruption):
		code = FailureDataCorruption
	case errors.Is(err, errInvalidOutput):
		code = FailureInvalidOutput
	case errors.Is(err, fs.ErrPermission), errors.Is(err, syscall.EACCES), errors.Is(err, syscall.EPERM):
		code = FailurePermissionDenied
	case errors.Is(err, syscall.EROFS):
		code = FailureReadOnly
	case errors.Is(err, syscall.ENOSPC):
		code = FailureInsufficientSpace
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, syscall.ENOENT):
		code = FailureNotFound
	}
	return &Failure{Code: code, Operation: operation, Message: err.Error()}
}

func cacheEvictionFailure(operation string, err error) *Failure {
	failure := measurementFailure(operation, err)
	if failure != nil && failure.Code == FailureIO {
		failure.Code = FailureCacheEviction
	}
	return failure
}

func CollectStorage(ctx context.Context) ([]StorageMeasurement, error) {
	partitions, err := disk.PartitionsWithContext(ctx, false)
	if err != nil {
		return nil, err
	}

	result := make([]StorageMeasurement, 0, len(partitions))
	seen := map[string]struct{}{}
	for _, partition := range partitions {
		if slices.Contains(partition.Opts, "ro") {
			continue
		}
		name := partition.Mountpoint
		if name == "" {
			name = partition.Device
		}
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		capacityGB := float64(0)
		if partition.Mountpoint != "" {
			if usage, usageErr := disk.UsageWithContext(ctx, partition.Mountpoint); usageErr == nil {
				capacityGB = toGB(usage.Total)
			}
		}
		result = append(result, StorageMeasurement{
			Name:         name,
			CapacityGB:   capacityGB,
			Mode:         "inventory",
			ReadSource:   SourceUnknown,
			ReadFailure:  notMeasuredFailure("storage_read", "run a storage benchmark to measure read throughput"),
			WriteSource:  SourceUnknown,
			WriteFailure: notMeasuredFailure("storage_write", "run a storage benchmark to measure write throughput"),
		})
	}
	return result, nil
}

func collectMemory(ctx context.Context) (*MemoryMeasurement, error) {
	info, infoErr := host.InfoWithContext(ctx)
	memory, memoryErr := mem.VirtualMemoryWithContext(ctx)
	measurement := &MemoryMeasurement{
		Source:  SourceUnknown,
		Failure: notMeasuredFailure("host_memory_copy", "run a memory benchmark to measure copy throughput"),
	}
	if info != nil {
		measurement.HostID = info.HostID
		measurement.Hostname = info.Hostname
	}
	if memory != nil {
		measurement.TotalGB = toGB(memory.Total)
	}
	return measurement, errors.Join(infoErr, memoryErr)
}
func toGB(value uint64) float64 { return float64(value) / 1_000_000_000 }

const benchmarkChunkSize = 8 * 1024 * 1024

func matchStorage(path string, storage []StorageMeasurement) *StorageMeasurement {
	path = filepath.Clean(path)
	var best *StorageMeasurement
	for i := range storage {
		candidate := &storage[i]
		if candidate.Name == "" {
			continue
		}
		mount := filepath.Clean(candidate.Name)
		if path == mount || pathHasMountPrefix(path, mount) {
			if best == nil || len(mount) > len(best.Name) {
				best = candidate
			}
		}
	}
	return best
}
func pathHasMountPrefix(path, mount string) bool {
	if mount == string(filepath.Separator) {
		return true
	}
	if strings.HasSuffix(mount, string(filepath.Separator)) {
		return strings.HasPrefix(path, mount)
	}
	return len(path) > len(mount) && path[:len(mount)] == mount && os.IsPathSeparator(path[len(mount)])
}

const numaSysfsPath = "/sys/devices/system/node"

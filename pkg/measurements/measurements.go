package measurements

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	mem "github.com/shirou/gopsutil/v4/mem"
)

const (
	SourceConfigured = "configured"
	SourceMeasured   = "measured"
	SourceUnknown    = "unknown"
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

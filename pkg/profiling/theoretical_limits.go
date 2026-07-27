package profiling

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/cedana/cedana/pkg/keys"
	"github.com/cedana/cedana/pkg/measurements"
	"github.com/cedana/cedana/pkg/style"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
)

const (
	LimitKindStorage         = "storage"
	LimitKindHostMemory      = "host_memory"
	LimitKindGPUDeviceMemory = "gpu_memory"
	LimitKindHostDeviceLink  = "host_device_link"
)

type TheoreticalLimit struct {
	Name           string                `json:"name"`
	Kind           string                `json:"kind"`
	Direction      string                `json:"direction,omitempty"`
	BytesPerSecond uint64                `json:"bytes_per_second,omitempty"`
	Source         string                `json:"source"`
	Confidence     string                `json:"confidence"`
	Device         string                `json:"device,omitempty"`
	Details        map[string]string     `json:"details,omitempty"`
	Failure        *measurements.Failure `json:"failure,omitempty"`
}

var systemTheoreticalLimits = struct {
	sync.RWMutex
	limits []*TheoreticalLimit
}{}

func BuildTheoreticalLimits(report *measurements.Report) []*TheoreticalLimit {
	if report == nil {
		return nil
	}
	limits := make([]*TheoreticalLimit, 0)
	for _, storage := range report.Storage {
		limits = append(limits,
			storageLimit(storage, "read", storage.ReadGBPerSec, storage.ReadSource, storage.ReadConfidence, storage.ReadFailure),
			storageLimit(storage, "write", storage.WriteGBPerSec, storage.WriteSource, storage.WriteConfidence, storage.WriteFailure),
		)
	}
	if memory := report.Memory; memory != nil {
		limits = append(limits, &TheoreticalLimit{
			Name: "host_memory", Kind: LimitKindHostMemory, Direction: "bidirectional",
			BytesPerSecond: rateBytes(memory.CopyGBPerSec), Source: sourceOrUnknown(memory.Source),
			Confidence: confidenceOrNone(memory.Confidence), Device: memory.HostID,
			Details: compactLimitDetails(map[string]string{
				"hostname": memory.Hostname, "cpu": memory.CPU,
				"placement":  memory.Placement,
				"cpu_vendor": memory.CPUVendor, "memory_gb": formatLimitFloat(memory.TotalGB),
				"benchmark_size_gb": formatLimitFloat(memory.BenchmarkSizeGB),
				"benchmark_runs":    formatLimitInt(memory.BenchmarkRuns),
			}),
			Failure: memory.Failure,
		})
	}
	for _, numa := range report.NUMA {
		if numa.Kind != "host_memory_copy" {
			continue
		}
		limits = append(limits, &TheoreticalLimit{
			Name: numa.Name, Kind: LimitKindHostMemory, Direction: "bidirectional",
			BytesPerSecond: rateBytes(numa.CopyGBPerSec), Source: sourceOrUnknown(numa.Source),
			Confidence: confidenceOrNone(numa.Confidence), Device: numa.Name,
			Details: compactLimitDetails(map[string]string{
				"cpu_node": strconv.Itoa(numa.CPUNode), "memory_node": strconv.Itoa(numa.MemoryNode),
				"cpus":     numa.CPUs,
				"locality": numa.Locality, "memory_gb": formatLimitFloat(numa.MemoryGB),
				"benchmark_size_gb": formatLimitFloat(numa.BenchmarkSizeGB),
				"benchmark_runs":    formatLimitInt(numa.BenchmarkRuns),
			}),
			Failure: numa.Failure,
		})
	}
	return limits
}

func storageLimit(storage measurements.StorageMeasurement, direction string, rate *float64, source, confidence string, failure *measurements.Failure) *TheoreticalLimit {
	return &TheoreticalLimit{
		Name: storage.Name, Kind: LimitKindStorage, Direction: direction,
		BytesPerSecond: rateBytes(rate), Source: sourceOrUnknown(source),
		Confidence: confidenceOrNone(confidence), Device: storage.Name,
		Details: compactLimitDetails(map[string]string{
			"backend": storage.Backend, "mode": storage.Mode, "cache_policy": storage.CachePolicy,
			"benchmark_size_gb": formatLimitFloat(storage.BenchmarkSizeGB),
			"benchmark_runs":    formatLimitInt(storage.BenchmarkRuns),
		}),
		Failure: failure,
	}
}

func SetSystemTheoreticalLimits(limits []*TheoreticalLimit) {
	systemTheoreticalLimits.Lock()
	defer systemTheoreticalLimits.Unlock()
	systemTheoreticalLimits.limits = cloneLimits(limits)
}

func SystemTheoreticalLimits() []*TheoreticalLimit {
	systemTheoreticalLimits.RLock()
	defer systemTheoreticalLimits.RUnlock()
	return cloneLimits(systemTheoreticalLimits.limits)
}

func AttachTheoreticalLimits(ctx context.Context, path string) {
	data, ok := ctx.Value(keys.PROFILING_CONTEXT_KEY).(*Data)
	if !ok || data == nil {
		return
	}
	limits := SystemTheoreticalLimits()
	storageDevice := matchedStorageDevice(path, limits)
	for _, limit := range limits {
		if limit.Kind == LimitKindStorage && limit.Device != storageDevice {
			continue
		}
		data.TheoreticalLimits = append(data.TheoreticalLimits, limit)
	}
}

func AddTheoreticalLimit(ctx context.Context, limit *TheoreticalLimit) {
	data, ok := ctx.Value(keys.PROFILING_CONTEXT_KEY).(*Data)
	if !ok || data == nil || limit == nil {
		return
	}
	data.TheoreticalLimits = append(data.TheoreticalLimits, limit)
}
func matchedStorageDevice(path string, limits []*TheoreticalLimit) string {
	var best string
	for _, limit := range limits {
		if limit == nil || limit.Kind != LimitKindStorage || limit.Device == "" {
			continue
		}
		if pathMatchesLimit(path, limit.Device) && len(limit.Device) > len(best) {
			best = limit.Device
		}
	}
	return best
}

func pathMatchesLimit(path, mount string) bool {
	if path == "" || mount == "" {
		return false
	}
	path = filepath.Clean(path)
	mount = filepath.Clean(mount)
	if path == mount {
		return true
	}
	if mount == string(filepath.Separator) {
		return filepath.IsAbs(path)
	}
	return strings.HasPrefix(path, mount+string(filepath.Separator))
}

func cloneLimits(limits []*TheoreticalLimit) []*TheoreticalLimit {
	result := make([]*TheoreticalLimit, 0, len(limits))
	for _, limit := range limits {
		if cloned := cloneLimit(limit); cloned != nil {
			result = append(result, cloned)
		}
	}
	return result
}

func cloneLimit(limit *TheoreticalLimit) *TheoreticalLimit {
	if limit == nil {
		return nil
	}
	cloned := *limit
	cloned.Details = cloneLimitDetails(limit.Details)
	cloned.Failure = cloneFailure(limit.Failure)
	return &cloned
}

func cloneLimitDetails(details map[string]string) map[string]string {
	if len(details) == 0 {
		return nil
	}
	result := make(map[string]string, len(details))
	for key, value := range details {
		result[key] = value
	}
	return result
}

func compactLimitDetails(details map[string]string) map[string]string {
	result := make(map[string]string, len(details))
	for key, value := range details {
		if value != "" {
			result[key] = value
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func cloneFailure(failure *measurements.Failure) *measurements.Failure {
	if failure == nil {
		return nil
	}
	cloned := *failure
	return &cloned
}

func rateBytes(rate *float64) uint64 {
	if rate == nil || *rate <= 0 {
		return 0
	}
	return uint64(*rate * 1_000_000_000)
}

func sourceOrUnknown(source string) string {
	if source == "" {
		return measurements.SourceUnknown
	}
	return source
}

func confidenceOrNone(confidence string) string {
	if confidence == "" {
		return measurements.ConfidenceNone
	}
	return confidence
}

func formatLimitInt(value int) string {
	if value <= 0 {
		return ""
	}
	return strconv.Itoa(value)
}

func formatLimitFloat(value float64) string {
	if value <= 0 {
		return ""
	}
	return strconv.FormatFloat(value, 'f', 3, 64)
}
func PrintTheoreticalLimits(data *Data) {
	if data == nil || len(data.TheoreticalLimits) == 0 {
		return
	}

	showFailures := false
	for _, limit := range data.TheoreticalLimits {
		if limit != nil && limit.Failure != nil && limit.Failure.Code != measurements.FailureNotMeasured {
			showFailures = true
			break
		}
	}

	fmt.Println("Theoretical limits")
	writer := table.NewWriter()
	writer.SetStyle(style.TableStyle)
	writer.SetOutputMirror(os.Stdout)
	header := table.Row{"limit", "kind", "direction", "max GB/s", "source", "device"}
	if showFailures {
		header = append(header, "result", "detail")
	}
	writer.AppendHeader(header)

	for _, limit := range data.TheoreticalLimits {
		if limit == nil {
			continue
		}
		row := table.Row{
			limit.Name,
			limit.Kind,
			limit.Direction,
			limitRateString(limit.BytesPerSecond),
			sourceOrUnknown(limit.Source),
			limit.Device,
		}
		if showFailures {
			result, detail := "", ""
			if limit.Failure != nil && limit.Failure.Code != measurements.FailureNotMeasured {
				result = limit.Failure.Code
				detail = limit.Failure.Message
			}
			row = append(row, result, detail)
		}
		writer.AppendRow(row)
	}

	writer.SetColumnConfigs([]table.ColumnConfig{
		{Number: 1, Align: text.AlignLeft, AlignHeader: text.AlignLeft},
		{Number: 2, Align: text.AlignLeft, AlignHeader: text.AlignLeft},
		{Number: 3, Align: text.AlignLeft, AlignHeader: text.AlignLeft},
		{Number: 4, Align: text.AlignRight, AlignHeader: text.AlignRight},
		{Number: 5, Align: text.AlignLeft, AlignHeader: text.AlignLeft},
		{Number: 6, Align: text.AlignLeft, AlignHeader: text.AlignLeft},
	})
	writer.Render()
	fmt.Println()
}

func limitRateString(bytesPerSecond uint64) string {
	if bytesPerSecond == 0 {
		return "n/a"
	}
	value := strconv.FormatFloat(float64(bytesPerSecond)/1_000_000_000, 'f', 1, 64)
	return strings.TrimRight(strings.TrimRight(value, "0"), ".")
}

package profiling

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/cedana/cedana/pkg/keys"
)

type TheoreticalLimit struct {
	Name   string `json:"name"`
	Value  uint64 `json:"value"`
	Unit   string `json:"unit"`
	Device string `json:"device,omitempty"`
}

func AddTheoreticalLimit(ctx context.Context, limit *TheoreticalLimit) {
	data, ok := ctx.Value(keys.PROFILING_CONTEXT_KEY).(*Data)
	if !ok || data == nil || limit == nil {
		return
	}
	data.TheoreticalLimits = append(data.TheoreticalLimits, limit)
}

func PrintTheoreticalLimits(data *Data) {
	if data == nil || len(data.TheoreticalLimits) == 0 {
		return
	}
	summary := theoreticalLimitsSummary(data.TheoreticalLimits)
	for _, line := range summary {
		fmt.Println(line)
	}
}

func theoreticalLimitsSummary(limits []*TheoreticalLimit) []string {
	limitsByDevice := make(map[string][]string)
	devices := make([]string, 0)
	for _, limit := range limits {
		if limit == nil {
			continue
		}
		if _, ok := limitsByDevice[limit.Device]; !ok {
			devices = append(devices, limit.Device)
		}
		limitsByDevice[limit.Device] = append(limitsByDevice[limit.Device], fmt.Sprintf("%s %s", limitDisplayName(limit.Name), limitRateString(limit)))
	}
	if len(devices) == 1 {
		return []string{fmt.Sprintf("%s limits: %s", devices[0], strings.Join(limitsByDevice[devices[0]], ", "))}
	}

	groups := make([]string, 0, len(devices))
	for _, device := range devices {
		groups = append(groups, fmt.Sprintf("%s limits: %s", device, strings.Join(limitsByDevice[device], ", ")))
	}
	return groups
}

func limitRateString(limit *TheoreticalLimit) string {
	if limit.Value == 0 || limit.Unit != "bytes_per_second" {
		return "n/a"
	}
	value := strconv.FormatFloat(float64(limit.Value)/1_000_000_000, 'f', 1, 64)
	return strings.TrimRight(strings.TrimRight(value, "0"), ".") + " GB/s"
}

func limitDisplayName(name string) string {
	switch name {
	case "gpu_device_memory":
		return "device memory"
	case "gpu_host_device_link":
		return "host link"
	default:
		return strings.ReplaceAll(name, "_", " ")
	}
}

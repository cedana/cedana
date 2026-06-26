package gpu

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	gpu_proto "buf.build/gen/go/cedana/cedana-gpu/protocolbuffers/go/gpu"
	"github.com/cedana/cedana/pkg/profiling"
)

var gpuFunctionPhaseNames = map[string]string{
	"dumpShareableHandleMetadata": "shareable_handles",
	"dumpContextlessCalls":        "contextless_calls",
	"dumpVirtualCudaMemory":       "virtual_memory",
	"dumpCudaMemory":              "gpu_memory",
	"dumpCudaCalls":               "cuda_calls",
	"dumpHostGpuMemory":           "host_memory",
	"restoreShareableHandles":     "shareable_handles",
	"replayContextlessCalls":      "contextless_calls",
	"restoreVirtualMemory":        "virtual_memory",
	"restoreMemory":               "gpu_memory",
	"restoreCalls":                "cuda_calls",
	"readHostMemory":              "host_memory",
}

type gpuDurationStats struct {
	count int
	min   int64
	max   int64
}

type gpuWorkerTimingRow struct {
	worker         *gpu_proto.WorkerProfile
	workerPosition int
	name           string
	durationNs     int64
	bytes          uint64
}

func addGPUFunctionProfileToProfiling(ctx context.Context, duration time.Duration, f ...any) context.Context {
	functionCtx := profiling.AddTimingParallelComponent(ctx, duration, f...)
	profiling.MarkRedundant(functionCtx)
	return functionCtx
}

// The generated GPU API still names these fields DurationMs, but the GPU
// controller now sends nanosecond values through them until the schema is renamed.
func gpuProfileDuration(durationNs int64) time.Duration {
	return time.Duration(durationNs) * time.Nanosecond
}

func gpuSortedWorkers(profile *gpu_proto.GpuProfile) []*gpu_proto.WorkerProfile {
	workers := append([]*gpu_proto.WorkerProfile(nil), profile.GetWorkers()...)
	sort.SliceStable(workers, func(i, j int) bool {
		return workers[i].GetWorkerIndex() < workers[j].GetWorkerIndex()
	})
	return workers
}

func gpuPhaseDisplayNames(profile *gpu_proto.GpuProfile) map[string]string {
	displayNames := make(map[string]string)
	for _, function := range profile.GetFunctions() {
		phaseName, ok := gpuFunctionPhaseNames[function.GetName()]
		if ok {
			displayNames[phaseName] = function.GetName()
		}
	}
	return displayNames
}

func gpuProfilePhaseOrder(profile *gpu_proto.GpuProfile, workers []*gpu_proto.WorkerProfile) []string {
	seen := make(map[string]bool)
	var phaseOrder []string

	for _, function := range profile.GetFunctions() {
		phaseName, ok := gpuFunctionPhaseNames[function.GetName()]
		if !ok || seen[phaseName] {
			continue
		}
		seen[phaseName] = true
		phaseOrder = append(phaseOrder, phaseName)
	}

	for _, worker := range workers {
		for _, phase := range worker.GetPhases() {
			phaseName := phase.GetName()
			if phaseName == "" || seen[phaseName] {
				continue
			}
			seen[phaseName] = true
			phaseOrder = append(phaseOrder, phaseName)
		}
	}

	return phaseOrder
}

func gpuWorkerLabel(worker *gpu_proto.WorkerProfile, position int) string {
	if worker.GetWorkerIndex() >= 0 {
		return fmt.Sprintf("w%d", worker.GetWorkerIndex()+1)
	}
	return fmt.Sprintf("w%d", position+1)
}

func gpuWorkerPhase(worker *gpu_proto.WorkerProfile, phaseName string) *gpu_proto.WorkerPhaseProfile {
	for _, phase := range worker.GetPhases() {
		if phase.GetName() == phaseName {
			return phase
		}
	}
	return nil
}

func gpuPhaseRows(workers []*gpu_proto.WorkerProfile, phaseName, displayName string) []gpuWorkerTimingRow {
	var rows []gpuWorkerTimingRow
	for i, worker := range workers {
		phase := gpuWorkerPhase(worker, phaseName)
		if phase == nil {
			continue
		}
		durationNs := phase.GetDurationMs()
		bytes := phase.GetBytes()
		if durationNs == 0 && bytes == 0 {
			continue
		}
		rows = append(rows, gpuWorkerTimingRow{
			worker:         worker,
			workerPosition: i,
			name:           displayName,
			durationNs:     durationNs,
			bytes:          bytes,
		})
	}
	return rows
}

func gpuOtherRows(workers []*gpu_proto.WorkerProfile) []gpuWorkerTimingRow {
	var rows []gpuWorkerTimingRow
	for i, worker := range workers {
		var namedDurationNs int64
		var namedBytes uint64
		for _, phase := range worker.GetPhases() {
			namedDurationNs += phase.GetDurationMs()
			namedBytes += phase.GetBytes()
		}

		otherDurationNs := worker.GetDurationMs() - namedDurationNs
		if otherDurationNs < 0 {
			otherDurationNs = 0
		}

		var otherBytes uint64
		if worker.GetBytes() > namedBytes {
			otherBytes = worker.GetBytes() - namedBytes
		}

		if otherDurationNs == 0 && otherBytes == 0 {
			continue
		}

		rows = append(rows, gpuWorkerTimingRow{
			worker:         worker,
			workerPosition: i,
			name:           "other",
			durationNs:     otherDurationNs,
			bytes:          otherBytes,
		})
	}
	return rows
}

func gpuWorkerDurationStats(rows []gpuWorkerTimingRow) gpuDurationStats {
	var stats gpuDurationStats
	for _, row := range rows {
		if stats.count == 0 || row.durationNs < stats.min {
			stats.min = row.durationNs
		}
		if stats.count == 0 || row.durationNs > stats.max {
			stats.max = row.durationNs
		}
		stats.count++
	}
	return stats
}

func gpuWorkerProfileTags(row gpuWorkerTimingRow, stats gpuDurationStats) []any {
	tags := []any{fmt.Sprintf("%s %s", gpuWorkerLabel(row.worker, row.workerPosition), row.name)}
	if row.worker.GetPID() != 0 {
		tags = append(tags, fmt.Sprintf("pid=%d", row.worker.GetPID()))
	}
	if stats.count > 1 {
		if row.durationNs == stats.min {
			tags = append(tags, "fastest")
		}
		if row.durationNs == stats.max {
			tags = append(tags, "slowest")
		}
	}
	return tags
}

func addGPUWorkerTimingRowToProfiling(ctx context.Context, row gpuWorkerTimingRow, stats gpuDurationStats) {
	functionCtx := addGPUFunctionProfileToProfiling(
		ctx,
		gpuProfileDuration(row.durationNs),
		gpuWorkerProfileTags(row, stats)...,
	)
	profiling.AddIO(functionCtx, int64(row.bytes))
	profiling.MarkIORedundant(functionCtx)
}

func addGPUWorkerTimingRowsToProfiling(ctx context.Context, rows []gpuWorkerTimingRow) {
	stats := gpuWorkerDurationStats(rows)
	for _, row := range rows {
		addGPUWorkerTimingRowToProfiling(ctx, row, stats)
	}
}

func addGPUCallProfilesToProfiling(ctx context.Context, profile *gpu_proto.GpuProfile) {
	var parts []any
	parts = append(parts, "calls")
	for _, fn := range profile.GetFunctions() {
		name := fn.GetName()
		if !strings.HasPrefix(name, "call:") {
			continue
		}
		// controller already sorted desc and truncated to top-5; render in order.
		parts = append(parts, fmt.Sprintf("%s=%s",
			strings.TrimPrefix(name, "call:"),
			(time.Duration(fn.GetDurationMs())*time.Millisecond).String()))
	}
	if len(parts) == 1 { // only the "calls" label, no call entries
		return
	}
	callCtx := profiling.AddTimingParallelComponent(ctx, 0, parts...)
	profiling.MarkRedundant(callCtx)
}

func addGPUProfileToProfiling(ctx context.Context, profile *gpu_proto.GpuProfile) {
	if profile == nil {
		return
	}

	workers := gpuSortedWorkers(profile)
	displayNames := gpuPhaseDisplayNames(profile)

	for _, phaseName := range gpuProfilePhaseOrder(profile, workers) {
		displayName := displayNames[phaseName]
		if displayName == "" {
			displayName = phaseName
		}
		addGPUWorkerTimingRowsToProfiling(ctx, gpuPhaseRows(workers, phaseName, displayName))

		if displayName == "restoreCalls" {
			addGPUCallProfilesToProfiling(ctx, profile)
		}
	}

	addGPUWorkerTimingRowsToProfiling(ctx, gpuOtherRows(workers))
}

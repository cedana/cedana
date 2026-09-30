package gpu

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
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
	"dumpMemVerify":               "mem_verify",
	"restoreShareableHandles":     "shareable_handles",
	"replayContextlessCalls":      "contextless_calls",
	"restoreVirtualMemory":        "virtual_memory",
	"restoreMemory":               "gpu_memory",
	"restoreCalls":                "cuda_calls",
	"restoreMemVerify":            "mem_verify",
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
	phase          *gpu_proto.WorkerPhaseProfile
	name           string
	durationNs     int64
	reference      gpuReference
	bytes          uint64
}

type gpuProfileInterval struct {
	startNs int64
	endNs   int64
}

const (
	gpuReferenceMinSamples = 5
	gpuReferenceMaxSamples = 8
)

type gpuReference struct {
	durationNs int64
	source     string
	samples    int
	key        string
}

type gpuReferenceSample struct {
	durationNs int64
	bytes      uint64
}

type gpuReferenceHistory struct {
	mu      sync.Mutex
	samples map[string][]gpuReferenceSample
}

var learnedGPUReferences = gpuReferenceHistory{samples: make(map[string][]gpuReferenceSample)}

func addGPUFunctionProfileToProfiling(ctx context.Context, duration time.Duration, f ...any) context.Context {
	functionCtx := profiling.AddTimingParallelComponent(ctx, duration, f...)
	profiling.MarkRedundant(functionCtx)
	return functionCtx
}

func gpuProfileDuration(durationNs int64) time.Duration {
	return time.Duration(durationNs) * time.Nanosecond
}

func gpuReferenceBucket(bytes uint64) int {
	if bytes == 0 {
		return -1
	}

	bucket := 0
	for bytes > 1 {
		bytes = (bytes + 1) >> 1
		bucket++
	}
	return bucket
}

func gpuProfileOperation(profile *gpu_proto.GpuProfile) string {
	for _, function := range profile.GetFunctions() {
		switch function.GetName() {
		case "dumpShareableHandleMetadata", "dumpContextlessCalls", "dumpVirtualCudaMemory",
			"dumpCudaMemory", "dumpCudaCalls", "dumpHostGpuMemory":
			return "dump"
		case "restoreShareableHandles", "replayContextlessCalls", "restoreVirtualMemory",
			"restoreMemory", "restoreCalls", "readHostMemory":
			return "restore"
		}
	}
	return "unknown"
}

func gpuWorkerShape(worker *gpu_proto.WorkerProfile) string {
	parts := make([]string, 0, len(worker.GetPhases()))
	for _, phase := range worker.GetPhases() {
		parts = append(parts, fmt.Sprintf("%s:%d", phase.GetName(), gpuReferenceBucket(phase.GetBytes())))
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

func gpuProfileWorkloadKey(profile *gpu_proto.GpuProfile, operation string) string {
	shapes := make([]string, 0, len(profile.GetWorkers()))
	for _, worker := range profile.GetWorkers() {
		shapes = append(shapes, gpuWorkerShape(worker))
	}
	sort.Strings(shapes)
	return fmt.Sprintf("%s|workers=%d|%s", operation, len(shapes), strings.Join(shapes, ";"))
}

func (history *gpuReferenceHistory) observe(key string, durationNs int64, bytes uint64) gpuReference {
	if key == "" || durationNs <= 0 {
		return gpuReference{key: key}
	}

	history.mu.Lock()
	defer history.mu.Unlock()

	samples := append(history.samples[key], gpuReferenceSample{durationNs: durationNs, bytes: bytes})
	if len(samples) > gpuReferenceMaxSamples {
		samples = samples[len(samples)-gpuReferenceMaxSamples:]
	}
	history.samples[key] = samples
	if len(samples) < gpuReferenceMinSamples {
		return gpuReference{samples: len(samples), key: key}
	}

	ordered := append([]gpuReferenceSample(nil), samples...)
	if bytes == 0 {
		sort.Slice(ordered, func(i, j int) bool {
			return ordered[i].durationNs < ordered[j].durationNs
		})
		return gpuReference{
			durationNs: ordered[(len(ordered)-1)/4].durationNs,
			source:     "learned",
			samples:    len(samples),
			key:        key,
		}
	}

	sort.Slice(ordered, func(i, j int) bool {
		left := float64(ordered[i].durationNs) / float64(ordered[i].bytes)
		right := float64(ordered[j].durationNs) / float64(ordered[j].bytes)
		return left < right
	})
	reference := ordered[(len(ordered)-1)/4]
	return gpuReference{
		durationNs: int64(math.Ceil(float64(bytes) * float64(reference.durationNs) / float64(reference.bytes))),
		source:     "learned",
		samples:    len(samples),
		key:        key,
	}
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

func gpuPhaseReference(profileKey string, worker *gpu_proto.WorkerProfile, workerPosition int, phaseName string, phase *gpu_proto.WorkerPhaseProfile) gpuReference {
	workerKey := worker.GetWorkerIndex()
	if workerKey < 0 {
		workerKey = int32(workerPosition)
	}
	profileKey = fmt.Sprintf("%s|worker=%d", profileKey, workerKey)
	key := fmt.Sprintf("%s|phase=%s|bytes=%d", profileKey, phaseName, gpuReferenceBucket(phase.GetBytes()))
	learned := learnedGPUReferences.observe(key, phase.GetDurationNs(), phase.GetBytes())
	if learned.durationNs > 0 {
		return learned
	}

	if phaseName == "gpu_memory" && phase.GetReferenceDurationNs() > 0 {
		return gpuReference{
			durationNs: phase.GetReferenceDurationNs(),
			source:     "gpu capability model",
			key:        key,
		}
	}
	return learned
}

func gpuPhaseRows(workers []*gpu_proto.WorkerProfile, profileKey, phaseName, displayName string) []gpuWorkerTimingRow {
	var rows []gpuWorkerTimingRow
	for i, worker := range workers {
		phase := gpuWorkerPhase(worker, phaseName)
		if phase == nil {
			continue
		}
		durationNs := phase.GetDurationNs()
		bytes := phase.GetBytes()
		if durationNs == 0 && bytes == 0 {
			continue
		}
		rows = append(rows, gpuWorkerTimingRow{
			worker:         worker,
			workerPosition: i,
			phase:          phase,
			name:           displayName,
			durationNs:     durationNs,
			reference:      gpuPhaseReference(profileKey, worker, i, phaseName, phase),
			bytes:          bytes,
		})
	}
	return rows
}

func gpuMergedIntervalDurationNs(intervals []gpuProfileInterval) int64 {
	if len(intervals) == 0 {
		return 0
	}

	sort.Slice(intervals, func(i, j int) bool {
		if intervals[i].startNs == intervals[j].startNs {
			return intervals[i].endNs < intervals[j].endNs
		}
		return intervals[i].startNs < intervals[j].startNs
	})

	var total int64
	currentStart := intervals[0].startNs
	currentEnd := intervals[0].endNs
	for _, interval := range intervals[1:] {
		if interval.endNs <= interval.startNs {
			continue
		}
		if interval.startNs <= currentEnd {
			if interval.endNs > currentEnd {
				currentEnd = interval.endNs
			}
			continue
		}

		total += currentEnd - currentStart
		currentStart = interval.startNs
		currentEnd = interval.endNs
	}

	return total + currentEnd - currentStart
}

func gpuOtherRows(workers []*gpu_proto.WorkerProfile) []gpuWorkerTimingRow {
	var rows []gpuWorkerTimingRow
	for i, worker := range workers {
		var namedBytes uint64
		var coveredIntervals []gpuProfileInterval
		intervalsComplete := true

		for _, phase := range worker.GetPhases() {
			namedBytes += phase.GetBytes()
			phaseIntervals := phase.GetIntervals()
			if phase.GetIntervalsTruncated() || (phase.GetDurationNs() > 0 && len(phaseIntervals) == 0) {
				intervalsComplete = false
			}
			for _, interval := range phaseIntervals {
				startNs := interval.GetStartNs()
				endNs := interval.GetEndNs()
				if startNs < 0 {
					startNs = 0
				}
				if endNs > worker.GetDurationNs() {
					endNs = worker.GetDurationNs()
				}
				if endNs > startNs {
					coveredIntervals = append(coveredIntervals, gpuProfileInterval{
						startNs: startNs,
						endNs:   endNs,
					})
				}
			}
		}

		if !intervalsComplete {
			continue
		}

		otherDurationNs := worker.GetDurationNs() - gpuMergedIntervalDurationNs(coveredIntervals)
		if otherDurationNs < 0 {
			otherDurationNs = 0
		}

		var otherBytes uint64
		if otherDurationNs > 0 && worker.GetBytes() > namedBytes {
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

func gpuReferenceIntervals(row gpuWorkerTimingRow) ([]gpuProfileInterval, bool) {
	if row.phase == nil || row.phase.GetIntervalsTruncated() || row.reference.durationNs <= 0 {
		return nil, false
	}

	var actualIntervals []gpuProfileInterval
	for _, interval := range row.phase.GetIntervals() {
		if interval.GetEndNs() > interval.GetStartNs() {
			actualIntervals = append(actualIntervals, gpuProfileInterval{
				startNs: interval.GetStartNs(),
				endNs:   interval.GetEndNs(),
			})
		}
	}
	actualDurationNs := gpuMergedIntervalDurationNs(actualIntervals)
	if actualDurationNs <= 0 {
		return nil, false
	}

	scale := float64(row.reference.durationNs) / float64(actualDurationNs)
	referenceIntervals := make([]gpuProfileInterval, 0, len(actualIntervals))
	for _, interval := range actualIntervals {
		durationNs := int64(math.Ceil(float64(interval.endNs-interval.startNs) * scale))
		referenceIntervals = append(referenceIntervals, gpuProfileInterval{
			startNs: interval.startNs,
			endNs:   interval.startNs + durationNs,
		})
	}
	return referenceIntervals, true
}

func gpuAggregateReference(rows []gpuWorkerTimingRow, key string) gpuReference {
	byWorker := make(map[*gpu_proto.WorkerProfile][]gpuWorkerTimingRow)
	for _, row := range rows {
		byWorker[row.worker] = append(byWorker[row.worker], row)
	}
	if len(byWorker) == 0 {
		return gpuReference{}
	}

	var aggregateNs int64
	for _, workerRows := range byWorker {
		var referenceIntervals []gpuProfileInterval
		for _, row := range workerRows {
			intervals, ok := gpuReferenceIntervals(row)
			if !ok {
				return gpuReference{}
			}
			referenceIntervals = append(referenceIntervals, intervals...)
		}
		workerReferenceNs := gpuMergedIntervalDurationNs(referenceIntervals)
		if workerReferenceNs <= 0 {
			return gpuReference{}
		}
		aggregateNs = max(aggregateNs, workerReferenceNs)
	}

	return gpuReference{
		durationNs: aggregateNs,
		source:     "gpu worker aggregate",
		key:        key,
	}
}

func addGPUAggregateReferenceToProfiling(ctx context.Context, rows []gpuWorkerTimingRow, key string) {
	reference := gpuAggregateReference(rows, key)
	if reference.durationNs <= 0 {
		return
	}

	var actualIntervals []gpuProfileInterval
	for _, row := range rows {
		for _, interval := range row.phase.GetIntervals() {
			if interval.GetEndNs() > interval.GetStartNs() {
				actualIntervals = append(actualIntervals, gpuProfileInterval{
					startNs: interval.GetStartNs(),
					endNs:   interval.GetEndNs(),
				})
			}
		}
	}
	actualNs := gpuMergedIntervalDurationNs(actualIntervals)
	if actualNs <= 0 {
		return
	}

	functionCtx := addGPUFunctionProfileToProfiling(ctx, gpuProfileDuration(actualNs), "GPU worker phases")
	profiling.SetReference(functionCtx, profiling.Reference{
		Duration: gpuProfileDuration(reference.durationNs),
		Source:   reference.source,
		Key:      reference.key,
	})
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
	if stats.count > 1 && stats.min != stats.max {
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
	if row.reference.durationNs > 0 {
		profiling.SetReference(functionCtx, profiling.Reference{
			Duration: gpuProfileDuration(row.reference.durationNs),
			Source:   row.reference.source,
			Samples:  row.reference.samples,
			Key:      row.reference.key,
		})
	}
	profiling.MarkIORedundant(functionCtx)
}

func addGPUWorkerTimingRowsToProfiling(ctx context.Context, rows []gpuWorkerTimingRow) {
	stats := gpuWorkerDurationStats(rows)
	for _, row := range rows {
		addGPUWorkerTimingRowToProfiling(ctx, row, stats)
	}
}

func addGPUProfileToProfiling(ctx context.Context, profile *gpu_proto.GpuProfile) {
	if profile == nil {
		return
	}

	workers := gpuSortedWorkers(profile)
	displayNames := gpuPhaseDisplayNames(profile)
	workloadKey := gpuProfileWorkloadKey(profile, gpuProfileOperation(profile))
	rowsByPhase := make([][]gpuWorkerTimingRow, 0)
	var namedRows []gpuWorkerTimingRow

	for _, phaseName := range gpuProfilePhaseOrder(profile, workers) {
		displayName := displayNames[phaseName]
		if displayName == "" {
			displayName = phaseName
		}

		rows := gpuPhaseRows(workers, workloadKey, phaseName, displayName)
		rowsByPhase = append(rowsByPhase, rows)
		namedRows = append(namedRows, rows...)
	}

	addGPUAggregateReferenceToProfiling(ctx, namedRows, workloadKey)
	for _, rows := range rowsByPhase {
		addGPUWorkerTimingRowsToProfiling(ctx, rows)
	}

	addGPUWorkerTimingRowsToProfiling(ctx, gpuOtherRows(workers))
}

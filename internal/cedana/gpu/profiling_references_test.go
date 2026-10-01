package gpu

import (
	"context"
	"fmt"
	"testing"

	gpu_proto "buf.build/gen/go/cedana/cedana-gpu/protocolbuffers/go/gpu"
	"github.com/cedana/cedana/pkg/keys"
	"github.com/cedana/cedana/pkg/profiling"
)

func TestGPUReferenceHistoryUsesBestSampleDuringWarmup(t *testing.T) {
	history := gpuReferenceHistory{samples: make(map[string][]gpuReferenceSample)}
	for index, duration := range []int64{120, 100, 130, 90} {
		reference := history.observe("dump|gpu_memory", duration, 100)
		if want := []int64{120, 100, 100, 90}[index]; reference.durationNs != want {
			t.Fatalf("warmup reference = %d, want %d", reference.durationNs, want)
		}
		if reference.source != "best so far" || reference.samples != index+1 {
			t.Fatalf("warmup provenance = %#v", reference)
		}
	}

	reference := history.observe("dump|gpu_memory", 110, 100)
	if reference.durationNs != 100 {
		t.Fatalf("reference duration = %d, want 100", reference.durationNs)
	}
	if reference.source != "learned" || reference.samples != gpuReferenceMinSamples {
		t.Fatalf("reference provenance = %#v", reference)
	}
}

func TestGPUReferenceHistoryScalesByteBearingSamples(t *testing.T) {
	history := gpuReferenceHistory{samples: make(map[string][]gpuReferenceSample)}
	for range gpuReferenceMinSamples {
		history.observe("dump|gpu_memory", 100, 100)
	}

	reference := history.observe("dump|gpu_memory", 220, 200)
	if reference.durationNs != 200 {
		t.Fatalf("reference duration = %d, want 200", reference.durationNs)
	}
}

func TestGPUReferenceHistoryDoesNotMixCompatibilityKeys(t *testing.T) {
	history := gpuReferenceHistory{samples: make(map[string][]gpuReferenceSample)}
	for range gpuReferenceMinSamples {
		history.observe("dump|workers=1|phase=gpu_memory|bytes=26", 100, 100)
	}

	reference := history.observe("dump|workers=2|phase=gpu_memory|bytes=26", 100, 100)
	if reference.durationNs != 100 || reference.source != "best so far" || reference.samples != 1 {
		t.Fatalf("incompatible reference = %#v", reference)
	}
}

func TestGPUPhaseReferencePrefersModeledGPUCopy(t *testing.T) {
	phase := &gpu_proto.WorkerPhaseProfile{
		DurationNs:          100,
		Bytes:               100,
		ReferenceDurationNs: 25,
	}

	reference := gpuPhaseReference("dump", &gpu_proto.WorkerProfile{}, 0, "gpu_memory", phase)
	if reference.durationNs != 25 || reference.source != "modeled" {
		t.Fatalf("gpu memory reference = %#v", reference)
	}
}

func TestGPUProfileOperationRecognizesVerification(t *testing.T) {
	for _, test := range []struct {
		name      string
		operation string
	}{
		{"dumpMemVerify", "dump"},
		{"restoreMemVerify", "restore"},
	} {
		profile := &gpu_proto.GpuProfile{
			Functions: []*gpu_proto.GpuFunctionProfile{{Name: test.name}},
		}
		if got := gpuProfileOperation(profile); got != test.operation {
			t.Fatalf("operation for %s = %s, want %s", test.name, got, test.operation)
		}
	}
}

func TestGPUAggregateLearnsMeasuredIntervals(t *testing.T) {
	previousSamples, previousKeys := learnedGPUReferences.samples, learnedGPUReferences.keys
	learnedGPUReferences.samples, learnedGPUReferences.keys = make(map[string][]gpuReferenceSample), nil
	t.Cleanup(func() { learnedGPUReferences.samples, learnedGPUReferences.keys = previousSamples, previousKeys })
	for _, test := range []struct {
		name        string
		secondStart int64
		want        int64
	}{
		{"serial", 100, 200}, {"parallel", 0, 100},
	} {
		t.Run(test.name, func(t *testing.T) {
			data := &profiling.Data{}
			ctx := context.WithValue(context.Background(), keys.PROFILING_CONTEXT_KEY, data)
			rows := []gpuWorkerTimingRow{
				{durationNs: 100, phase: &gpu_proto.WorkerPhaseProfile{Intervals: []*gpu_proto.WorkerPhaseInterval{{StartNs: 0, EndNs: 100}}}, reference: gpuReference{durationNs: 200}},
				{durationNs: 100, phase: &gpu_proto.WorkerPhaseProfile{Intervals: []*gpu_proto.WorkerPhaseInterval{{StartNs: test.secondStart, EndNs: test.secondStart + 100}}}, reference: gpuReference{durationNs: 200}},
			}
			addGPUAggregateReferenceToProfiling(ctx, rows, test.name)
			if len(data.Components) != 1 {
				t.Fatalf("aggregate rows = %d", len(data.Components))
			}
			row := data.Components[0]
			if row.Duration != test.want || row.ReferenceDuration != test.want || row.ReferenceSamples != 1 {
				t.Fatalf("aggregate = %#v, want measured duration %d", row, test.want)
			}
			rows[1].phase.Intervals[0].EndNs += 100
			addGPUAggregateReferenceToProfiling(ctx, rows, test.name)
			if row := data.Components[1]; row.ReferenceDuration != test.want || row.ReferenceSamples != 2 {
				t.Fatalf("aggregate did not reuse measured history: %#v", row)
			}
			rows[0].phase.IntervalsTruncated = true
			addGPUAggregateReferenceToProfiling(ctx, rows, test.name)
			if len(data.Components) != 2 {
				t.Fatal("truncated intervals produced an aggregate")
			}
		})
	}
}

func TestGPUReferenceHistoryBoundsKeys(t *testing.T) {
	history := gpuReferenceHistory{samples: make(map[string][]gpuReferenceSample)}
	for i := 0; i < gpuReferenceMaxKeys; i++ {
		history.observe(fmt.Sprint(i), 100, 100)
	}
	history.observe("0", 90, 100)
	if len(history.keys) != gpuReferenceMaxKeys {
		t.Fatal("updating a key changed retention")
	}
	history.observe("new", 100, 100)
	if len(history.samples) != gpuReferenceMaxKeys {
		t.Fatal("history exceeded key limit")
	}
	if _, exists := history.samples["0"]; exists {
		t.Fatal("oldest key was not evicted")
	}
	if reference := history.observe("0", 200, 100); reference.durationNs != 200 || reference.samples != 1 {
		t.Fatal("evicted history was reused")
	}
}

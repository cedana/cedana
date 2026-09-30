package gpu

import (
	"testing"

	gpu_proto "buf.build/gen/go/cedana/cedana-gpu/protocolbuffers/go/gpu"
)

func TestGPUReferenceHistoryWaitsForStableSamples(t *testing.T) {
	history := gpuReferenceHistory{samples: make(map[string][]gpuReferenceSample)}
	for _, duration := range []int64{120, 100, 130, 90} {
		reference := history.observe("dump|gpu_memory", duration, 100)
		if reference.durationNs != 0 {
			t.Fatalf("reference before warmup = %d", reference.durationNs)
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
	if reference.durationNs != 0 || reference.samples != 1 {
		t.Fatalf("incompatible reference = %#v", reference)
	}
}

func TestGPUAggregateReferencePreservesParallelPhases(t *testing.T) {
	worker := &gpu_proto.WorkerProfile{}
	rows := []gpuWorkerTimingRow{
		{
			worker: worker,
			phase: &gpu_proto.WorkerPhaseProfile{
				Intervals: []*gpu_proto.WorkerPhaseInterval{{StartNs: 0, EndNs: 100}},
			},
			reference: gpuReference{durationNs: 50},
		},
		{
			worker: worker,
			phase: &gpu_proto.WorkerPhaseProfile{
				Intervals: []*gpu_proto.WorkerPhaseInterval{{StartNs: 0, EndNs: 100}},
			},
			reference: gpuReference{durationNs: 50},
		},
	}

	reference := gpuAggregateReference(rows, "dump")
	if reference.durationNs != 50 {
		t.Fatalf("parallel aggregate = %d, want 50", reference.durationNs)
	}
}

func TestGPUAggregateReferenceUsesSlowestWorker(t *testing.T) {
	first := &gpu_proto.WorkerProfile{}
	second := &gpu_proto.WorkerProfile{}
	rows := []gpuWorkerTimingRow{
		{
			worker: first,
			phase: &gpu_proto.WorkerPhaseProfile{
				Intervals: []*gpu_proto.WorkerPhaseInterval{{StartNs: 0, EndNs: 100}},
			},
			reference: gpuReference{durationNs: 50},
		},
		{
			worker: second,
			phase: &gpu_proto.WorkerPhaseProfile{
				Intervals: []*gpu_proto.WorkerPhaseInterval{{StartNs: 0, EndNs: 100}},
			},
			reference: gpuReference{durationNs: 80},
		},
	}

	reference := gpuAggregateReference(rows, "dump")
	if reference.durationNs != 80 {
		t.Fatalf("worker aggregate = %d, want 80", reference.durationNs)
	}
}

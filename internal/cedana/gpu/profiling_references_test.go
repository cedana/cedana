package gpu

import (
	"context"
	"testing"

	gpu_proto "buf.build/gen/go/cedana/cedana-gpu/protocolbuffers/go/gpu"
	"github.com/cedana/cedana/pkg/keys"
	"github.com/cedana/cedana/pkg/profiling"
)

func TestGPUReferencesUseExistingWorkerRows(t *testing.T) {
	profile := &gpu_proto.GpuProfile{Workers: []*gpu_proto.WorkerProfile{
		{WorkerIndex: 0, PID: 123, DurationNs: 100, Bytes: 100, Phases: []*gpu_proto.WorkerPhaseProfile{
			{Name: "gpu_memory", DurationNs: 100, Bytes: 100, ReferenceDurationNs: 25,
				Intervals: []*gpu_proto.WorkerPhaseInterval{{StartNs: 0, EndNs: 100}}},
		}},
		{WorkerIndex: 1, PID: 456, DurationNs: 100, Bytes: 100, Phases: []*gpu_proto.WorkerPhaseProfile{
			{Name: "gpu_memory", DurationNs: 100, Bytes: 100, ReferenceDurationNs: 50,
				Intervals: []*gpu_proto.WorkerPhaseInterval{{StartNs: 0, EndNs: 100}}},
		}},
	}}
	data := &profiling.Data{}
	ctx := context.WithValue(context.Background(), keys.PROFILING_CONTEXT_KEY, data)
	addGPUProfileToProfiling(ctx, profile)
	if len(data.Components) != 2 {
		t.Fatalf("expected individual workers only, got %d rows", len(data.Components))
	}
	for index, want := range []int64{25, 50} {
		row := data.Components[index]
		if row.ReferenceDuration != want || row.ReferenceSource != "modeled" || row.IO != 100 {
			t.Fatalf("worker %d reference = %#v", index, row)
		}
	}
}

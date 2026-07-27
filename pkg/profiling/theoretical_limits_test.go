package profiling

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/cedana/cedana/pkg/keys"
	"github.com/cedana/cedana/pkg/measurements"
)

func TestBuildTheoreticalLimitsPreservesModeAndFailure(t *testing.T) {
	rate := 2.5
	report := &measurements.Report{
		Storage: []measurements.StorageMeasurement{
			{
				Name: "/checkpoints", Mode: measurements.StorageModeCold,
				BenchmarkSizeGB: 0.5, BenchmarkRuns: 1,
				ReadGBPerSec: &rate, ReadSource: measurements.SourceMeasured,
				ReadConfidence:  measurements.ConfidenceMedium,
				WriteSource:     measurements.SourceUnknown,
				WriteConfidence: measurements.ConfidenceNone,
				WriteFailure:    &measurements.Failure{Code: measurements.FailureReadOnly, Message: "read-only filesystem"},
			},
		},
	}

	limits := BuildTheoreticalLimits(report)
	if len(limits) != 2 {
		t.Fatalf("limits = %d, want 2", len(limits))
	}
	if limits[0].BytesPerSecond != 2_500_000_000 ||
		limits[0].Details["mode"] != measurements.StorageModeCold ||
		limits[0].Details["benchmark_size_gb"] != "0.500" || limits[0].Details["benchmark_runs"] != "1" {
		t.Fatalf("read limit = %#v", limits[0])
	}
	if limits[1].Failure == nil || limits[1].Failure.Code != measurements.FailureReadOnly {
		t.Fatalf("write failure = %#v", limits[1].Failure)
	}
}

func TestAttachTheoreticalLimitsSelectsOperationMount(t *testing.T) {
	t.Cleanup(func() { SetSystemTheoreticalLimits(nil) })
	SetSystemTheoreticalLimits([]*TheoreticalLimit{
		{Name: "/", Kind: LimitKindStorage, Direction: "read", Device: "/"},
		{Name: "/mnt/checkpoints", Kind: LimitKindStorage, Direction: "read", Device: "/mnt/checkpoints"},
		{Name: "host_memory", Kind: LimitKindHostMemory},
	})
	data := &Data{Name: "dump"}
	ctx := context.WithValue(context.Background(), keys.PROFILING_CONTEXT_KEY, data)
	AttachTheoreticalLimits(ctx, "/mnt/checkpoints/job-1")

	if len(data.TheoreticalLimits) != 2 {
		t.Fatalf("limits = %#v, want matched storage plus host memory", data.TheoreticalLimits)
	}
	for _, limit := range data.TheoreticalLimits {
		if limit.Kind == LimitKindStorage && limit.Device != "/mnt/checkpoints" {
			t.Fatalf("attached wrong storage limit: %#v", limit)
		}
	}
}

func TestFlattenLiftsTheoreticalLimitsToRoot(t *testing.T) {
	child := &Data{
		Name: "storage",
		TheoreticalLimits: []*TheoreticalLimit{
			{Name: "storage", Kind: LimitKindStorage, Device: "/checkpoints"},
		},
	}
	data := &Data{Name: "dump", Components: []*Data{child}}

	Flatten(data)

	if len(data.TheoreticalLimits) != 1 || data.TheoreticalLimits[0].Device != "/checkpoints" {
		t.Fatalf("root limits = %#v", data.TheoreticalLimits)
	}
	if len(child.TheoreticalLimits) != 0 {
		t.Fatalf("child limits were not cleared: %#v", child.TheoreticalLimits)
	}
}
func TestTheoreticalLimitsGobRoundTrip(t *testing.T) {
	data := &Data{
		Name: "restore",
		TheoreticalLimits: []*TheoreticalLimit{
			{Name: "gpu_host_device_link", Kind: LimitKindHostDeviceLink, BytesPerSecond: 31_500_000_000},
		},
	}
	var buffer bytes.Buffer
	if err := Encode(data, &buffer); err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(buffer.String())
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.TheoreticalLimits) != 1 || decoded.TheoreticalLimits[0].BytesPerSecond != 31_500_000_000 {
		t.Fatalf("decoded limits = %#v", decoded.TheoreticalLimits)
	}
}

func TestTheoreticalLimitsJSONRoundTripPreservesIO(t *testing.T) {
	original := &Data{
		Name: "dump",
		IO:   4096,
		TheoreticalLimits: []*TheoreticalLimit{
			{
				Name: "host_memory", Kind: LimitKindHostMemory,
				Failure: &measurements.Failure{Code: measurements.FailureNotMeasured, Message: "benchmark not requested"},
			},
		},
	}
	encoded, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Data
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.IO != original.IO {
		t.Fatalf("decoded IO = %d, want %d", decoded.IO, original.IO)
	}
	if len(decoded.TheoreticalLimits) != 1 || decoded.TheoreticalLimits[0].Failure == nil {
		t.Fatalf("decoded limits = %#v", decoded.TheoreticalLimits)
	}
}

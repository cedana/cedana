package profiling

import (
	"bytes"
	"context"
	"encoding/json"
	"slices"
	"testing"

	"github.com/cedana/cedana/pkg/keys"
)

func TestAddTheoreticalLimit(t *testing.T) {
	data := &Data{Name: "dump"}
	ctx := context.WithValue(context.Background(), keys.PROFILING_CONTEXT_KEY, data)
	AddTheoreticalLimit(ctx, &TheoreticalLimit{
		Name: "gpu_host_device_link", Value: 31_500_000_000,
		Unit: "bytes_per_second", Device: "GPU-1",
	})

	if len(data.TheoreticalLimits) != 1 || data.TheoreticalLimits[0].Device != "GPU-1" {
		t.Fatalf("limits = %#v", data.TheoreticalLimits)
	}
}

func TestTheoreticalLimitDisplay(t *testing.T) {
	limit := &TheoreticalLimit{Name: "gpu_host_device_link", Value: 31_500_000_000, Unit: "bytes_per_second"}
	if got := limitDisplayName(limit.Name); got != "host link" {
		t.Fatalf("display name = %q", got)
	}
	if got := limitRateString(limit); got != "31.5 GB/s" {
		t.Fatalf("rate = %q", got)
	}
}

func TestTheoreticalLimitsSummaryGroupsMultipleDevices(t *testing.T) {
	limits := []*TheoreticalLimit{
		{Name: "gpu_device_memory", Value: 2_039_000_000_000, Unit: "bytes_per_second", Device: "GPU-1"},
		{Name: "gpu_host_device_link", Value: 31_500_000_000, Unit: "bytes_per_second", Device: "GPU-1"},
		{Name: "gpu_device_memory", Value: 1_555_000_000_000, Unit: "bytes_per_second", Device: "GPU-2"},
		{Name: "gpu_host_device_link", Value: 63_000_000_000, Unit: "bytes_per_second", Device: "GPU-2"},
	}
	got := theoreticalLimitsSummary(limits)
	want := []string{
		"GPU-1 limits: device memory 2039 GB/s, host link 31.5 GB/s",
		"GPU-2 limits: device memory 1555 GB/s, host link 63 GB/s",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("summary = %#v, want %#v", got, want)
	}
}

func TestFlattenLiftsTheoreticalLimitsToRoot(t *testing.T) {
	child := &Data{
		TheoreticalLimits: []*TheoreticalLimit{{Name: "gpu_device_memory"}},
	}
	data := &Data{Name: "dump", Components: []*Data{child}}
	Flatten(data)

	if len(data.TheoreticalLimits) != 1 || len(child.TheoreticalLimits) != 0 {
		t.Fatalf("limits = %#v child = %#v", data.TheoreticalLimits, child.TheoreticalLimits)
	}
}

func TestTheoreticalLimitsRoundTrip(t *testing.T) {
	data := &Data{TheoreticalLimits: []*TheoreticalLimit{{
		Name: "gpu_device_memory", Value: 2_039_000_000_000, Unit: "bytes_per_second",
	}}}

	var buffer bytes.Buffer
	if err := Encode(data, &buffer); err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(buffer.String())
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.TheoreticalLimits) != 1 || decoded.TheoreticalLimits[0].Value != data.TheoreticalLimits[0].Value {
		t.Fatalf("decoded limits = %#v", decoded.TheoreticalLimits)
	}

	encoded, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	var decodedJSON Data
	if err := json.Unmarshal(encoded, &decodedJSON); err != nil {
		t.Fatal(err)
	}
	if len(decodedJSON.TheoreticalLimits) != 1 || decodedJSON.TheoreticalLimits[0].Value != data.TheoreticalLimits[0].Value {
		t.Fatalf("decoded JSON limits = %#v", decodedJSON.TheoreticalLimits)
	}
}

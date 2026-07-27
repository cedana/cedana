package gpu

import (
	"testing"

	gpu_proto "buf.build/gen/go/cedana/cedana-gpu/protocolbuffers/go/gpu"
)

func TestGPUTheoreticalLimitFromAPI(t *testing.T) {
	limit := gpuTheoreticalLimit(&gpu_proto.ThroughputCapability{
		Name:           "gpu_host_device_link",
		Kind:           "host_device_link",
		Direction:      "bidirectional",
		BytesPerSecond: 31_507_692_304,
		Source:         "theoretical",
		Confidence:     "high",
		Device:         "GPU-1",
		Details:        map[string]string{"pcie_speed_gt_s": "16", "pcie_width_max": "16"},
	})

	if limit == nil {
		t.Fatal("expected limit")
	}
	if limit.Kind != "host_device_link" || limit.BytesPerSecond != 31_507_692_304 {
		t.Fatalf("unexpected limit: %#v", limit)
	}
	if limit.Device != "GPU-1" || limit.Details["pcie_speed_gt_s"] != "16" {
		t.Fatalf("unexpected device details: %#v", limit)
	}
}

func TestGPUTheoreticalLimitPreservesFailure(t *testing.T) {
	limit := gpuTheoreticalLimit(&gpu_proto.ThroughputCapability{
		Name:           "gpu_device_memory",
		Kind:           "gpu_memory",
		Source:         "unknown",
		Confidence:     "none",
		FailureCode:    "unsupported",
		FailureMessage: "maximum memory clock or bus width is unavailable from NVML",
	})

	if limit.Failure == nil || limit.Failure.Code != "unsupported" {
		t.Fatalf("unexpected failure: %#v", limit.Failure)
	}
}

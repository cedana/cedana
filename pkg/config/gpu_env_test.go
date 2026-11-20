package config

import (
	"slices"
	"testing"
)

func TestGPUClientEnv(t *testing.T) {
	saved := Global.GPU
	t.Cleanup(func() { Global.GPU = saved })

	Global.GPU.ImageZeroCopy = true
	Global.GPU.ProfilingEnabled = true
	if got := GPUClientEnv(nil); !slices.Equal(got, []string{"CEDANA_GPU_IMAGE_ZERO_COPY=1", "CEDANA_GPU_PROFILING_ENABLED=1"}) {
		t.Errorf("on: got %v", got)
	}

	Global.GPU.ImageZeroCopy = false
	Global.GPU.ProfilingEnabled = false
	if got := GPUClientEnv([]string{"PATH=/bin"}); !slices.Equal(got, []string{"CEDANA_GPU_IMAGE_ZERO_COPY=0", "CEDANA_GPU_PROFILING_ENABLED=0"}) {
		t.Errorf("off: got %v", got)
	}

	// The workload's own value wins
	if got := GPUClientEnv([]string{"CEDANA_GPU_IMAGE_ZERO_COPY=1"}); !slices.Equal(got, []string{"CEDANA_GPU_PROFILING_ENABLED=0"}) {
		t.Errorf("workload override: got %v", got)
	}
}

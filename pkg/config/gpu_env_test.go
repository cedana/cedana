package config

import (
	"slices"
	"testing"
)

func TestGPUClientEnv(t *testing.T) {
	saved := Global.GPU.ImageZeroCopy
	t.Cleanup(func() { Global.GPU.ImageZeroCopy = saved })

	Global.GPU.ImageZeroCopy = true
	if got := GPUClientEnv(nil); !slices.Equal(got, []string{"CEDANA_GPU_IMAGE_ZERO_COPY=1"}) {
		t.Errorf("zero copy on: got %v", got)
	}

	Global.GPU.ImageZeroCopy = false
	if got := GPUClientEnv([]string{"PATH=/bin"}); !slices.Equal(got, []string{"CEDANA_GPU_IMAGE_ZERO_COPY=0"}) {
		t.Errorf("zero copy off: got %v", got)
	}

	// The workload's own value wins
	if got := GPUClientEnv([]string{"CEDANA_GPU_IMAGE_ZERO_COPY=1"}); len(got) != 0 {
		t.Errorf("workload override: got %v, want nothing added", got)
	}
}

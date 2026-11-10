package config

import (
	"slices"
	"testing"

	"github.com/spf13/viper"
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

func TestInitReadsGPUKastSettingsFromEnvironment(t *testing.T) {
	originalGlobal := Global
	originalDir := Dir
	t.Cleanup(func() {
		viper.Reset()
		Global = originalGlobal
		Dir = originalDir
		setDefaults()
		bindEnvVars()
	})
	t.Setenv("CEDANA_GPU_KAST_ENABLED", "true")
	t.Setenv("CEDANA_GPU_CONTROLLER_ENV", "KAST_W=/var/lib/cedana/kast/W.json KAST_ARENA=window")

	viper.Reset()
	Global = originalGlobal
	setDefaults()
	bindEnvVars()

	if err := Init(Args{ConfigDir: t.TempDir()}); err != nil {
		t.Fatalf("init config: %v", err)
	}
	if !Global.GPU.KastEnabled {
		t.Fatalf("kast_enabled not read from the environment")
	}
	want := []string{"KAST_W=/var/lib/cedana/kast/W.json", "KAST_ARENA=window"}
	if !slices.Equal(Global.GPU.ControllerEnv, want) {
		t.Fatalf("controller_env = %v, want %v", Global.GPU.ControllerEnv, want)
	}
}

package gpu

import (
	"testing"

	"buf.build/gen/go/cedana/cedana-gpu/protocolbuffers/go/gpu"
	"github.com/cedana/cedana/pkg/config"
)

// The GPU controller gets the daemon's one verification mode on each request
func TestGPUVerifyModeFollowsTheDaemonsMode(t *testing.T) {
	previous := config.Global.Checkpoint.ChecksumVerify
	t.Cleanup(func() { config.Global.Checkpoint.ChecksumVerify = previous })
	for mode, want := range map[string]gpu.VerifyMode{
		"off":     gpu.VerifyMode_VERIFY_OFF,
		"warn":    gpu.VerifyMode_VERIFY_WARN,
		"strict":  gpu.VerifyMode_VERIFY_STRICT,
		"STRICT ": gpu.VerifyMode_VERIFY_STRICT,
		"other":   gpu.VerifyMode_VERIFY_WARN,
	} {
		config.Global.Checkpoint.ChecksumVerify = mode
		if got := gpuVerifyMode(); got != want {
			t.Errorf("%q: got %v, want %v", mode, got, want)
		}
	}
	config.Global.Checkpoint.ChecksumVerify = ""
	if got := gpuVerifyMode(); got == gpu.VerifyMode_VERIFY_UNSPECIFIED {
		t.Error("the daemon never sends VERIFY_UNSPECIFIED")
	}
}

package gpu

// The daemon's one verification mode, Checkpoint.ChecksumVerify, drives the GPU
// controller's MemVerify as well as the mem-verify CRIU plugin and the daemon's own
// compare (cedana-designs/mem-verify.org, decision 8).

import (
	"buf.build/gen/go/cedana/cedana-gpu/protocolbuffers/go/gpu"
	"github.com/cedana/cedana/pkg/config"
	"github.com/rs/zerolog"
)

// gpuVerifyMode maps the daemon's mode to the controller's. The controller's own
// default, CEDANA_GPU_VERIFY, applies only to VERIFY_UNSPECIFIED, which the daemon
// no longer sends.
func gpuVerifyMode() gpu.VerifyMode {
	switch config.ChecksumVerifyMode() {
	case config.CHECKSUM_VERIFY_STRICT:
		return gpu.VerifyMode_VERIFY_STRICT
	case config.CHECKSUM_VERIFY_WARN:
		return gpu.VerifyMode_VERIFY_WARN
	default:
		return gpu.VerifyMode_VERIFY_OFF
	}
}

// logVerifySummary logs what the controller recorded at dump, or compared at restore
func logVerifySummary(log zerolog.Logger, op string, s *gpu.VerifySummary) {
	if s == nil || s.GetMode() == gpu.VerifyMode_VERIFY_OFF || s.GetMode() == gpu.VerifyMode_VERIFY_UNSPECIFIED {
		return
	}
	event := log.Info()
	if s.GetMismatched() > 0 || s.GetUnverified() > 0 || s.GetIncomparable() {
		event = log.Warn()
	}
	event.Str("op", op).Str("mode", s.GetMode().String()).Uint64("expected", s.GetExpected()).Uint64("verified", s.GetVerified()).
		Uint64("mismatched", s.GetMismatched()).Uint64("unverified", s.GetUnverified()).Uint64("unhashed", s.GetUnhashed()).
		Uint64("bytes", s.GetBytes()).Bool("incomparable", s.GetIncomparable()).Strs("failures", s.GetFailures()).
		Msg("GPU state verification")
}

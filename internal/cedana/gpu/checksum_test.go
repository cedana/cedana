package gpu

import (
	"context"
	"testing"

	"buf.build/gen/go/cedana/cedana-gpu/protocolbuffers/go/gpu"
	"github.com/cedana/cedana/pkg/keys"
)

func TestCheckChecksum(t *testing.T) {
	summary := func(mode gpu.VerifyMode, checksum string) *gpu.VerifySummary {
		s := &gpu.VerifySummary{}
		s.SetMode(mode)
		s.SetChecksum(checksum)
		return s
	}
	tests := []struct {
		name    string
		want    string
		verify  *gpu.VerifySummary
		wantErr bool
	}{
		{"no expected checksum", "", summary(gpu.VerifyMode_VERIFY_STRICT, "sha256:b"), false},
		{"strict match", "sha256:a", summary(gpu.VerifyMode_VERIFY_STRICT, "sha256:a"), false},
		{"strict mismatch", "sha256:a", summary(gpu.VerifyMode_VERIFY_STRICT, "sha256:b"), true},
		{"strict no sidecars read", "sha256:a", summary(gpu.VerifyMode_VERIFY_STRICT, ""), true},
		{"warn mismatch", "sha256:a", summary(gpu.VerifyMode_VERIFY_WARN, "sha256:b"), false},
		{"verify off", "sha256:a", summary(gpu.VerifyMode_VERIFY_OFF, ""), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.WithValue(context.Background(), keys.GPU_EXPECTED_CHECKSUM_CONTEXT_KEY, tt.want)
			if err := checkChecksum(ctx, tt.verify); (err != nil) != tt.wantErr {
				t.Fatalf("checkChecksum() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

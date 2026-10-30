package cedana

import (
	"errors"
	"testing"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/client"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestAFailedRestoresVerificationTravelsInTheError(t *testing.T) {
	resp := &daemon.RestoreResp{
		Checksum:       "crc32c:00000000",
		ChecksumResult: daemon.ChecksumResult_CHECKSUM_MISMATCH,
		ChecksumReason: daemon.ChecksumReason_REASON_STORED,
		ChecksumStrict: true,
	}
	err := withRestoreDetails(status.Error(codes.FailedPrecondition, "checkpoint checksum mismatch"), resp)
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("the code must be kept, got %v", err)
	}
	got := client.RestoreRespFromError(err)
	if got == nil || got.Checksum != resp.Checksum || got.ChecksumResult != resp.ChecksumResult || got.ChecksumReason != resp.ChecksumReason || !got.ChecksumStrict {
		t.Fatalf("details = %v", got)
	}

	// Nothing checked: the error is left as it is
	plain := withRestoreDetails(errors.New("boom"), &daemon.RestoreResp{})
	if client.RestoreRespFromError(plain) != nil {
		t.Fatal("an unchecked restore attaches nothing")
	}
}

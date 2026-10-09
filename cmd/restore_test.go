package cmd

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/pkg/flags"
	"github.com/cedana/cedana/pkg/keys"
	"google.golang.org/protobuf/encoding/protojson"
)

func TestRestoreChecksumFlagSetsTheRequest(t *testing.T) {
	cmd := restoreCmd
	t.Cleanup(func() {
		cmd.PersistentFlags().Set(flags.ChecksumFlag.Full, "")
		cmd.PersistentFlags().Set(flags.NoServerFlag.Full, "false")
	})
	if err := cmd.PersistentFlags().Set(flags.NoServerFlag.Full, "true"); err != nil {
		t.Fatal(err)
	}
	if err := cmd.PersistentFlags().Set(flags.ChecksumFlag.Full, "crc32c:8ef7d855"); err != nil {
		t.Fatal(err)
	}
	cmd.SetContext(context.Background())
	if err := cmd.PersistentPreRunE(cmd, nil); err != nil {
		t.Fatalf("pre-run: %v", err)
	}
	req, ok := cmd.Context().Value(keys.RESTORE_REQ_CONTEXT_KEY).(*daemon.RestoreReq)
	if !ok {
		t.Fatal("no restore request in the context")
	}
	if req.Checksum != "crc32c:8ef7d855" {
		t.Fatalf("request checksum = %q", req.Checksum)
	}
}

func TestRestoreResultFileHoldsTheVerification(t *testing.T) {
	cmd := restoreCmd
	path := filepath.Join(t.TempDir(), "result.json")
	t.Cleanup(func() { cmd.PersistentFlags().Set(flags.ResultFileFlag.Full, "") })
	if err := cmd.PersistentFlags().Set(flags.ResultFileFlag.Full, path); err != nil {
		t.Fatal(err)
	}
	resp := &daemon.RestoreResp{
		Checksum:       "crc32c:00000000",
		ChecksumResult: daemon.ChecksumResult_CHECKSUM_MISMATCH,
		ChecksumReason: daemon.ChecksumReason_REASON_STORED,
	}
	if err := writeRestoreResult(cmd, resp); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := &daemon.RestoreResp{}
	if err := protojson.Unmarshal(data, got); err != nil {
		t.Fatalf("the file is not a RestoreResp: %v: %s", err, data)
	}
	if got.Checksum != resp.Checksum || got.ChecksumResult != resp.ChecksumResult || got.ChecksumReason != resp.ChecksumReason {
		t.Fatalf("file holds %s", data)
	}
	t.Logf("result file: %s", data)

	// Without the flag nothing is written
	cmd.PersistentFlags().Set(flags.ResultFileFlag.Full, "")
	if err := writeRestoreResult(cmd, resp); err != nil {
		t.Fatal(err)
	}
}

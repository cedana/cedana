package eventstream

import (
	"context"
	"encoding/json"
	"testing"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/plugins/containerd"
	"github.com/cedana/cedana/pkg/config"
	"github.com/cedana/cedana/pkg/profiling"
)

func TestCheckpointInfoPreservesLegacyAndCurrentPayloads(t *testing.T) {
	data := &profiling.Data{}
	info := checkpointInfo{
		ActionId:      "checkpoint-action",
		Status:        "success",
		ProfilingInfo: profilingInfo{Raw: data, TotalDuration: 42, TotalIO: 7},
		Info:          info{Profiling: data, TotalDuration: 42, TotalIO: 7},
	}

	payload, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(payload, &fields); err != nil {
		t.Fatal(err)
	}
	if _, ok := fields["profiling_info"]; !ok {
		t.Fatal("checkpoint payload must retain profiling_info for existing propagators")
	}
	if _, ok := fields["info"]; !ok {
		t.Fatal("checkpoint payload must include info for restore telemetry consumers")
	}
}

func TestCheckpointErrorDoesNotReplaceActionID(t *testing.T) {
	info := checkpointInfo{
		ActionId: "checkpoint-action",
		Status:   "error",
		Info:     info{Error: "dump failed"},
	}

	payload, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}

	var decoded checkpointInfo
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.ActionId != "checkpoint-action" {
		t.Fatalf("action ID changed during serialization: %q", decoded.ActionId)
	}
	if decoded.Info.Error != "dump failed" {
		t.Fatalf("terminal error missing from payload: %q", decoded.Info.Error)
	}
}

func TestCheckpointReqForCluster(t *testing.T) {
	ptr := func(s string) *string { return &s }
	cases := []struct {
		name    string
		req     *string
		daemon  string
		matches bool
	}{
		{"same cluster", ptr("a"), "a", true},
		{"other cluster", ptr("b"), "a", false},
		{"request without cluster", nil, "a", true},
		{"request with empty cluster", ptr(""), "a", true},
		{"daemon without cluster", ptr("b"), "", true},
	}
	for _, c := range cases {
		req := checkpointReq{ClusterId: c.req}
		if got := req.forCluster(c.daemon); got != c.matches {
			t.Errorf("%s: forCluster = %v, want %v", c.name, got, c.matches)
		}
	}
}
func TestCheckpointInfoUploadPending(t *testing.T) {
	info := checkpointInfo{
		ActionId:      "checkpoint-action",
		Status:        "success",
		Path:          "s3://checkpoints/dump.tar.lz4",
		UploadPending: true,
	}

	payload, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(payload, &fields); err != nil {
		t.Fatal(err)
	}
	if string(fields["upload_pending"]) != "true" {
		t.Fatalf("checkpoint payload must say that the upload is pending: %s", fields["upload_pending"])
	}

	info.UploadPending = false

	payload, err = json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}

	fields = map[string]json.RawMessage{}
	if err := json.Unmarshal(payload, &fields); err != nil {
		t.Fatal(err)
	}
	if _, ok := fields["upload_pending"]; ok {
		t.Fatal("checkpoint payload must omit upload_pending for a checkpoint that is already uploaded")
	}
}

func TestNewDumpReqAsyncOnlyWithChecksums(t *testing.T) {
	checksum := config.Global.Checkpoint.Checksum
	t.Cleanup(func() { config.Global.Checkpoint.Checksum = checksum })
	ctx := context.Background()
	container := &containerd.Containerd{ID: "app"}

	t.Run("ChecksumOn", func(t *testing.T) {
		config.Global.Checkpoint.Checksum = true
		req := newDumpReq(ctx, "checkpoint", container, nil)
		if !req.GetAsync() {
			t.Fatal("with checksums on, the compress, the upload and the checksum must run after the dump")
		}
		if req.GetName() != "checkpoint" || req.GetDetails().GetContainerd() != container {
			t.Fatalf("unexpected request %v", req)
		}
	})

	t.Run("ChecksumOff", func(t *testing.T) {
		config.Global.Checkpoint.Checksum = false
		if newDumpReq(ctx, "checkpoint", container, nil).GetAsync() {
			t.Fatal("with checksums off, the dump must be synchronous")
		}
	})

	t.Run("OverridesTakePrecedence", func(t *testing.T) {
		config.Global.Checkpoint.Checksum = true
		req := newDumpReq(ctx, "checkpoint", container, &checkpointOverrides{Async: false, Streams: 4, Compression: "lz4"})
		if req.GetAsync() || req.GetStreams() != 4 || req.GetCompression() != "lz4" {
			t.Fatalf("expected the overrides in the request, got async=%v streams=%d compression=%q",
				req.GetAsync(), req.GetStreams(), req.GetCompression())
		}

		config.Global.Checkpoint.Checksum = false
		if !newDumpReq(ctx, "checkpoint", container, &checkpointOverrides{Async: true}).GetAsync() {
			t.Fatal("an override can ask for an asynchronous dump with checksums off")
		}
	})
}

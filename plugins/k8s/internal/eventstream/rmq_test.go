package eventstream

import (
	"encoding/json"
	"testing"

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

func TestCheckpointInfoChecksum(t *testing.T) {
	info := checkpointInfo{
		ActionId: "checkpoint-action",
		Status:   "success",
		Path:     "cedana://checkpoints/dump.tar.lz4",
		Checksum: "sha256:abc123",
	}

	payload, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(payload, &fields); err != nil {
		t.Fatal(err)
	}
	if string(fields["checksum"]) != `"sha256:abc123"` {
		t.Fatalf("checkpoint payload must carry the checksum: %s", fields["checksum"])
	}

	info.Checksum = ""

	payload, err = json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}

	fields = map[string]json.RawMessage{}
	if err := json.Unmarshal(payload, &fields); err != nil {
		t.Fatal(err)
	}
	if _, ok := fields["checksum"]; ok {
		t.Fatal("checkpoint payload must omit the checksum when none was computed")
	}
}

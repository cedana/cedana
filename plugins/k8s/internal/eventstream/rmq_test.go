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

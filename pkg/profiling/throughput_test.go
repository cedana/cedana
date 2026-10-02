package profiling

import (
	"bytes"
	"context"
	"encoding/gob"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/cedana/cedana/pkg/keys"
)

func resetReferenceHistory(t *testing.T) {
	previousSamples, previousKeys := learnedReferences.samples, learnedReferences.keys
	learnedReferences.samples, learnedReferences.keys = make(map[string][]referenceSample), nil
	t.Cleanup(func() { learnedReferences.samples, learnedReferences.keys = previousSamples, previousKeys })
}

func TestReferenceHistoryUsesPriorBestThenQuartile(t *testing.T) {
	history := learnedReferenceHistory{samples: make(map[string][]referenceSample)}
	for index, duration := range []int64{120, 100, 130, 90, 110, 200} {
		reference, samples := history.observe("row", duration, 0)
		want := []int64{0, 120, 100, 100, 90, 100}[index]
		if reference != want || samples != index {
			t.Fatalf("sample %d: reference=%d samples=%d, want %d/%d", index, reference, samples, want, index)
		}
	}
}

func TestReferenceHistoryScalesGPUBytes(t *testing.T) {
	history := learnedReferenceHistory{samples: make(map[string][]referenceSample)}
	history.observe("gpu", 100, 100)
	if duration, samples := history.observe("gpu", 300, 200); duration != 200 || samples != 1 {
		t.Fatalf("scaled reference=%d samples=%d, want 200/1", duration, samples)
	}
}

func TestGPUFallbackUsesSharedHistory(t *testing.T) {
	resetReferenceHistory(t)
	for index, size := range []uint64{100, 110} {
		row := &Data{Name: "w1 gpu memory", IO: int64(size), Duration: int64(size) * 2}
		ctx := context.WithValue(context.Background(), keys.PROFILING_CONTEXT_KEY, row)
		SetModeledReference(ctx, 0, size)
		ApplyLearnedReferences(&Data{Components: []*Data{row}}, "dump")
		if row.ReferenceSamples != index || (index == 1 && row.ReferenceDuration != 220) {
			t.Fatalf("GPU fallback = %#v", row)
		}
	}
}

func TestReferencesSeparateRepeatedRows(t *testing.T) {
	resetReferenceHistory(t)
	profile := func(first, second int64) *Data {
		return &Data{Components: []*Data{
			{Name: "external-files:criu.QueryExtFiles", Duration: first},
			{Name: "external-files:criu.QueryExtFiles", Duration: second},
		}}
	}
	first := profile(10, 100)
	ApplyLearnedReferences(first, "dump")
	for _, row := range first.Components {
		if row.ReferenceDuration != 0 || row.ReferenceSamples != 0 {
			t.Fatalf("first execution learned from another occurrence: %#v", row)
		}
	}
	second := profile(20, 200)
	ApplyLearnedReferences(second, "dump")
	for index, want := range []int64{10, 100} {
		row := second.Components[index]
		if row.ReferenceDuration != want || row.ReferenceSamples != 1 || row.ReferenceSource != "best so far" {
			t.Fatalf("occurrence %d = %#v", index, row)
		}
	}
}

func TestReferencesPreserveModelAndNormalizeTags(t *testing.T) {
	resetReferenceHistory(t)
	for index, name := range []string{"w1 restoreMemory (pid=123, fastest)", "w1 restoreMemory (pid=456, slowest)"} {
		data := &Data{Components: []*Data{
			{Name: name, Duration: 100},
			{Name: "modeled", Duration: 50, ReferenceDuration: 10, ReferenceSource: "modeled"},
		}}
		ApplyLearnedReferences(data, "restore")
		if row := data.Components[0]; row.ReferenceSamples != index {
			t.Fatalf("tags changed history: %#v", row)
		}
		if row := data.Components[1]; row.ReferenceDuration != 10 || row.ReferenceSource != "modeled" {
			t.Fatalf("model overwritten: %#v", row)
		}
	}
}

func TestReferencesSeparateWorkloadShapesAndOperations(t *testing.T) {
	resetReferenceHistory(t)
	profile := func(duration, size int64, extraWorker bool) *Data {
		data := &Data{Components: []*Data{
			{Name: "criu.Dump", Duration: duration},
			{Name: "w1 dumpMemory (pid=123)", IO: size, Duration: 10, ReferenceDuration: 5},
		}}
		if extraWorker {
			data.Components = append(data.Components, &Data{Name: "w2 dumpMemory (pid=456)", IO: size, Duration: 10})
		}
		return data
	}
	ApplyLearnedReferences(profile(100, 64<<20, false), "dump")
	similar := profile(200, 65<<20, false)
	ApplyLearnedReferences(similar, "dump")
	if row := similar.Components[0]; row.ReferenceDuration != 100 || row.ReferenceSamples != 1 {
		t.Fatalf("similar shape lost history: %#v", row)
	}
	for _, data := range []*Data{profile(300, 4<<30, false), profile(400, 64<<20, true)} {
		ApplyLearnedReferences(data, "dump")
		if data.Components[0].ReferenceSamples != 0 {
			t.Fatal("different workload reused history")
		}
	}
	restore := profile(500, 64<<20, false)
	ApplyLearnedReferences(restore, "restore")
	if restore.Components[0].ReferenceSamples != 0 {
		t.Fatal("restore reused dump history")
	}
}

func TestReferenceHistoryBoundsKeysAndSamples(t *testing.T) {
	history := learnedReferenceHistory{samples: make(map[string][]referenceSample)}
	for i := range learnedReferenceMaxKeys {
		history.observe(fmt.Sprint(i), 100, 0)
	}
	for range learnedReferenceMaxSamples + 2 {
		history.observe("0", 90, 0)
	}
	if len(history.samples["0"]) != learnedReferenceMaxSamples || len(history.keys) != learnedReferenceMaxKeys {
		t.Fatal("history bounds changed")
	}
	history.observe("new", 100, 0)
	if _, exists := history.samples["0"]; exists {
		t.Fatal("oldest key was not evicted")
	}
	if duration, samples := history.observe("0", 200, 0); duration != 0 || samples != 0 {
		t.Fatal("evicted history was reused")
	}
}

func TestReferenceSerialization(t *testing.T) {
	data := &Data{ReferenceDuration: 25, ReferenceSource: "learned", ReferenceSamples: 5, referenceBytes: 100}
	var encoded bytes.Buffer
	if err := Encode(data, &encoded); err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(encoded.String())
	if err != nil {
		t.Fatal(err)
	}
	json, err := EncodeJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	fromJSON, err := DecodeJSON(json)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range []*Data{decoded, fromJSON} {
		if row.ReferenceDuration != 25 || row.ReferenceSource != "learned" || row.ReferenceSamples != 5 || row.referenceBytes != 0 {
			t.Fatalf("serialized reference = %#v", row)
		}
	}
	if strings.Contains(json, "reference_key") {
		t.Fatal("internal key exposed in JSON")
	}
	encoded.Reset()
	if err := gob.NewEncoder(&encoded).Encode(struct {
		Name     string
		Duration int64
	}{"old trace", 10}); err != nil {
		t.Fatal(err)
	}
	old, err := Decode(encoded.String())
	if err != nil {
		t.Fatal(err)
	}
	if old.ReferenceDuration != 0 || old.Duration != 10 {
		t.Fatalf("legacy trace = %#v", old)
	}
}

func TestReferenceDisplayDistinguishesHistory(t *testing.T) {
	data := &Data{Duration: int64(200 * time.Millisecond), ReferenceDuration: int64(100 * time.Millisecond)}
	if rendered := referenceDurationString(data, "ms"); !strings.Contains(rendered, "100ms") || strings.Contains(rendered, "~") || strings.Contains(rendered, "x)") {
		t.Fatalf("modeled display = %q", rendered)
	}
	data.ReferenceSamples = 1
	if rendered := referenceDurationString(data, "ms"); !strings.Contains(rendered, "~") {
		t.Fatalf("historical display = %q", rendered)
	}
}

type referenceTestCloser struct{}

func (referenceTestCloser) Read([]byte) (int, error)    { return 0, io.EOF }
func (referenceTestCloser) Write(p []byte) (int, error) { return len(p), nil }
func (referenceTestCloser) Close() error                { return nil }

func TestParallelIOCloseAccumulatesDuration(t *testing.T) {
	data := &Data{}
	ctx := context.WithValue(context.Background(), keys.PROFILING_CONTEXT_KEY, data)
	wrapped := IOParallelCategory[io.ReadWriteCloser](ctx, referenceTestCloser{}, "storage", "shard")
	row := data.Components[0].Components[0]
	row.Duration = int64(time.Hour)
	row.IO = 123
	if err := wrapped.Close(); err != nil {
		t.Fatal(err)
	}
	if row.Duration < int64(time.Hour) || row.IO != 123 {
		t.Fatalf("Close overwrote transfer accounting: %#v", row)
	}
}

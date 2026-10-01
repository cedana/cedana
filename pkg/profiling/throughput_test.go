package profiling

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cedana/cedana/pkg/keys"
)

func TestSetReferenceDuration(t *testing.T) {
	data := &Data{}
	ctx := context.WithValue(context.Background(), keys.PROFILING_CONTEXT_KEY, data)

	SetReferenceDuration(ctx, 25*time.Millisecond)

	if data.ReferenceDuration != int64(25*time.Millisecond) {
		t.Fatalf("reference duration = %s", time.Duration(data.ReferenceDuration))
	}
}

func TestReferenceProvenanceRoundTripsGob(t *testing.T) {
	var encoded bytes.Buffer
	if err := Encode(&Data{
		ReferenceDuration: int64(25 * time.Millisecond),
		ReferenceSource:   "learned",
		ReferenceSamples:  5,
		ReferenceKey:      "dump|gpu_memory",
	}, &encoded); err != nil {
		t.Fatal(err)
	}

	decoded, err := Decode(encoded.String())
	if err != nil {
		t.Fatal(err)
	}
	if decoded.ReferenceSource != "learned" || decoded.ReferenceSamples != 5 || decoded.ReferenceKey == "" {
		t.Fatalf("decoded reference provenance = %#v", decoded)
	}
}

func TestReferenceDurationStringMarksOutlier(t *testing.T) {
	data := &Data{Duration: int64(200 * time.Millisecond), ReferenceDuration: int64(100 * time.Millisecond)}

	if rendered := referenceDurationString(data, "ms"); !strings.Contains(rendered, "2.0x") {
		t.Fatalf("reference duration = %q", rendered)
	}
}

func TestSetReferenceRecordsProvenance(t *testing.T) {
	data := &Data{}
	ctx := context.WithValue(context.Background(), keys.PROFILING_CONTEXT_KEY, data)

	SetReference(ctx, Reference{
		Duration: 25 * time.Millisecond,
		Source:   "learned",
		Samples:  5,
		Key:      "dump|gpu_memory",
	})

	if data.ReferenceSource != "learned" || data.ReferenceSamples != 5 || data.ReferenceKey == "" {
		t.Fatalf("reference provenance = %#v", data)
	}
}

func TestApplyLearnedReferencesUsesBestThenLowerQuartile(t *testing.T) {
	previous := learnedReferences
	learnedReferences = learnedReferenceHistory{samples: make(map[string][]int64)}
	t.Cleanup(func() { learnedReferences = previous })

	for index, duration := range []int64{120, 100, 130, 90, 110} {
		data := &Data{Components: []*Data{
			{
				Name:     "validation.ValidateDumpRequest",
				Duration: duration,
			},
		}}
		ApplyLearnedReferences(data, "dump")
		reference := data.Components[0]
		want := []int64{120, 100, 100, 90, 100}[index]
		if reference.ReferenceDuration != want {
			t.Fatalf("reference after %d samples = %d, want %d", index+1, reference.ReferenceDuration, want)
		}
		if index < learnedReferenceMinSamples-1 && reference.ReferenceSource != "best so far" {
			t.Fatalf("warmup source = %q", reference.ReferenceSource)
		}
	}
}

func TestApplyLearnedReferencesPreservesModeledReferences(t *testing.T) {
	data := &Data{Components: []*Data{
		{
			Name:              "gpu memory",
			Duration:          int64(50 * time.Millisecond),
			ReferenceDuration: int64(10 * time.Millisecond),
			ReferenceSource:   "modeled",
		},
	}}

	ApplyLearnedReferences(data, "dump")
	reference := data.Components[0]
	if reference.ReferenceDuration != int64(10*time.Millisecond) || reference.ReferenceSource != "modeled" {
		t.Fatalf("modeled reference = %#v", reference)
	}
}

func TestApplyLearnedReferencesNormalizesPIDAndWorkerTags(t *testing.T) {
	previous := learnedReferences
	learnedReferences = learnedReferenceHistory{samples: make(map[string][]int64)}
	t.Cleanup(func() { learnedReferences = previous })

	for _, name := range []string{
		"w1 restoreMemory (pid=123, fastest)",
		"w1 restoreMemory (pid=456, slowest)",
	} {
		data := &Data{Components: []*Data{{Name: name, Duration: int64(time.Millisecond)}}}
		ApplyLearnedReferences(data, "restore")
	}

	if len(learnedReferences.samples) != 1 {
		t.Fatalf("reference keys = %#v", learnedReferences.samples)
	}
}

func TestReferenceProvenanceRoundTripsJSON(t *testing.T) {
	encoded, err := EncodeJSON(&Data{
		ReferenceDuration: int64(25 * time.Millisecond),
		ReferenceSource:   "learned",
		ReferenceSamples:  5,
		ReferenceKey:      "dump|gpu_memory",
	})
	if err != nil {
		t.Fatal(err)
	}

	decoded, err := DecodeJSON(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.ReferenceSource != "learned" || decoded.ReferenceSamples != 5 || decoded.ReferenceKey == "" {
		t.Fatalf("decoded reference provenance = %#v", decoded)
	}
}

func TestLearnedReferencesSeparateProfileShapes(t *testing.T) {
	previous := learnedReferences
	learnedReferences = learnedReferenceHistory{samples: make(map[string][]int64)}
	t.Cleanup(func() { learnedReferences = previous })

	profile := func(duration, io int64, extraWorker bool) *Data {
		data := &Data{Components: []*Data{
			{Name: "criu.Dump", Duration: duration},
			{Name: "w1 dumpMemory (pid=123)", Duration: 10, IO: io, ReferenceDuration: 5, ReferenceSource: "modeled"},
		}}
		if extraWorker {
			data.Components = append(data.Components, &Data{Name: "w2 dumpMemory (pid=456)", Duration: 10, IO: io})
		}
		return data
	}

	small := profile(100, 64<<20, false)
	ApplyLearnedReferences(small, "dump")
	similar := profile(200, 65<<20, false)
	ApplyLearnedReferences(similar, "dump")
	if row := similar.Components[0]; row.ReferenceDuration != 100 || row.ReferenceSamples != 2 {
		t.Fatalf("similar profile did not reuse history: %#v", row)
	}
	for _, data := range []*Data{profile(300, 4<<30, false), profile(400, 64<<20, true)} {
		ApplyLearnedReferences(data, "dump")
		if row := data.Components[0]; row.ReferenceDuration != row.Duration || row.ReferenceSamples != 1 {
			t.Fatalf("different profile reused history: %#v", row)
		}
	}
	otherOperation := profile(500, 64<<20, false)
	ApplyLearnedReferences(otherOperation, "restore")
	if otherOperation.Components[0].ReferenceSamples != 1 {
		t.Fatal("restore reused dump history")
	}
}

func TestLearnedReferenceProfileKeyIgnoresOrderAndTiming(t *testing.T) {
	first := &Data{Components: []*Data{
		{Name: "criu.Dump", Duration: 100},
		{Name: "w1 dumpMemory (pid=123, fastest)", IO: 64 << 20},
	}}
	second := &Data{Components: []*Data{
		{Name: "w1 dumpMemory (pid=456, slowest)", IO: 65 << 20, ReferenceDuration: 50},
		{Name: "criu.Dump", Duration: 200},
	}}
	if learnedReferenceProfileKey(first) != learnedReferenceProfileKey(second) {
		t.Fatal("row ordering, timing, or dynamic tags changed the profile key")
	}
}

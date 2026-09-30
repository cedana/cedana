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

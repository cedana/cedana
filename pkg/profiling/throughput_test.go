package profiling

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/cedana/cedana/pkg/keys"
)

func TestThroughputLimitDerivesMinDuration(t *testing.T) {
	data := &Data{}
	ctx := context.WithValue(context.Background(), keys.PROFILING_CONTEXT_KEY, data)
	SetThroughputLimit(ctx, ThroughputLimit{
		MaxThroughput: 100,
		Source:        "measured",
		Resource:      "mount:/checkpoints",
		Direction:     "write",
	})
	AddIO(ctx, 250)

	if data.MinDuration != (2500 * time.Millisecond).Nanoseconds() {
		t.Fatalf("min duration = %s", time.Duration(data.MinDuration))
	}
	if data.Tags[ThroughputSourceTag] != "measured" || data.Tags[ThroughputResourceTag] != "mount:/checkpoints" || data.Tags[ThroughputDirectionTag] != "write" {
		t.Fatalf("tags = %#v", data.Tags)
	}
}

func TestContextThroughputLimitAppliesToCurrentComponent(t *testing.T) {
	data := &Data{}
	ctx := context.WithValue(context.Background(), keys.PROFILING_CONTEXT_KEY, data)
	ctx = WithThroughputLimit(ctx, ThroughputLimit{MaxThroughput: 100})
	ApplyContextThroughputLimit(ctx)

	if data.MaxThroughput != 100 {
		t.Fatalf("max throughput = %d", data.MaxThroughput)
	}
}

func TestHasData(t *testing.T) {
	if HasData(context.Background()) {
		t.Fatal("empty context has profiling data")
	}
	ctx := context.WithValue(context.Background(), keys.PROFILING_CONTEXT_KEY, &Data{})
	if !HasData(ctx) {
		t.Fatal("profiling context was not detected")
	}
}

func TestIOComponentInheritsThroughputLimit(t *testing.T) {
	data := &Data{}
	ctx := context.WithValue(context.Background(), keys.PROFILING_CONTEXT_KEY, data)
	ctx = WithThroughputLimit(ctx, ThroughputLimit{MaxThroughput: 100})
	reader := IOComponent(ctx, io.NopCloser(strings.NewReader("hello")), "storageRead")
	if _, err := io.ReadAll(reader); err != nil {
		t.Fatal(err)
	}

	if len(data.Components) != 1 {
		t.Fatalf("components = %d", len(data.Components))
	}
	if data.Components[0].MaxThroughput != 100 || data.Components[0].MinDuration <= 0 {
		t.Fatalf("component = %#v", data.Components[0])
	}
}

func TestAddIOComponentInheritsThroughputLimit(t *testing.T) {
	data := &Data{}
	ctx := context.WithValue(context.Background(), keys.PROFILING_CONTEXT_KEY, data)
	ctx = WithThroughputLimit(ctx, ThroughputLimit{MaxThroughput: 100})
	AddIOComponent(ctx, 250, "storageRead")

	if len(data.Components) != 1 {
		t.Fatalf("components = %d", len(data.Components))
	}
	if data.Components[0].MaxThroughput != 100 || data.Components[0].MinDuration != (2500*time.Millisecond).Nanoseconds() {
		t.Fatalf("component = %#v", data.Components[0])
	}
}

func TestStorageTransferUsesCurrentProfileComponent(t *testing.T) {
	data := &Data{}
	ctx := context.WithValue(context.Background(), keys.PROFILING_CONTEXT_KEY, data)
	ctx = WithThroughputLimit(ctx, ThroughputLimit{MaxThroughput: 100})
	ctx = WithStorageTransfer(ctx)
	AddStorageTransfer(ctx, 250)

	if data.IO != 250 || data.MaxThroughput != 100 || data.MinDuration != (2500*time.Millisecond).Nanoseconds() {
		t.Fatalf("data = %#v", data)
	}
}

func TestThroughputFieldsRoundTrip(t *testing.T) {
	data := &Data{
		IO:            250,
		MaxThroughput: 100,
		MinDuration:   (2500 * time.Millisecond).Nanoseconds(),
		Tags:          map[string]string{ThroughputSourceTag: "measured"},
	}

	var buffer bytes.Buffer
	if err := Encode(data, &buffer); err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(buffer.String())
	if err != nil {
		t.Fatal(err)
	}
	if decoded.MaxThroughput != data.MaxThroughput || decoded.MinDuration != data.MinDuration {
		t.Fatalf("decoded = %#v", decoded)
	}

	encoded, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	var decodedJSON Data
	if err := json.Unmarshal(encoded, &decodedJSON); err != nil {
		t.Fatal(err)
	}
	if decodedJSON.Tags[ThroughputSourceTag] != "measured" {
		t.Fatalf("decoded tags = %#v", decodedJSON.Tags)
	}
}

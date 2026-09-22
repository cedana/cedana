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

type nopWriteCloser struct {
	io.Writer
}

func (nopWriteCloser) Close() error {
	return nil
}

type testNUMAThroughputLimiter struct {
	throughput int64
	resource   string
	ok         bool
}

func (l testNUMAThroughputLimiter) LimitForPID(context.Context, uint32) (int64, string, bool) {
	return l.throughput, l.resource, l.ok
}

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

func TestNUMAThroughputLimit(t *testing.T) {
	ctx := WithNUMAThroughput(context.Background(), testNUMAThroughputLimiter{
		throughput: 100,
		resource:   "numa0->numa1",
		ok:         true,
	})
	limit := NUMAThroughputLimit(ctx, 42)
	if limit == nil || limit.MaxThroughput != 100 || limit.Resource != "numa0->numa1" || limit.Source != "measured" {
		t.Fatalf("limit = %#v", limit)
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

func TestObservedThroughputAppliesToStorageCategory(t *testing.T) {
	cache := NewObservedThroughputCache(0, time.Minute)
	cache.record(observedThroughputKey{
		resource:  "s3://checkpoints/prod",
		direction: "write",
		shape:     "upload",
	}, 100, int64(time.Second))

	data := &Data{}
	ctx := context.WithValue(context.Background(), keys.PROFILING_CONTEXT_KEY, data)
	ctx = WithObservedThroughput(ctx, cache, "s3://checkpoints/prod", "write")
	var rawWriter io.WriteCloser = nopWriteCloser{Writer: io.Discard}
	writer := IOCategory(ctx, rawWriter, "storage", "upload")
	if _, err := writer.Write(make([]byte, 25)); err != nil {
		t.Fatal(err)
	}

	component := data.Components[0].Components[0]
	if component.ObservedThroughput != 100 || component.ObservedDuration != (250*time.Millisecond).Nanoseconds() {
		t.Fatalf("component = %#v", component)
	}
	if component.Tags[ObservedResourceTag] != "s3://checkpoints/prod" || component.Tags[ObservedDirectionTag] != "write" {
		t.Fatalf("tags = %#v", component.Tags)
	}
}

func TestObservedThroughputRecordsSuccessfulComponents(t *testing.T) {
	cache := NewObservedThroughputCache(0, time.Minute)
	data := &Data{Components: []*Data{{
		Name:     "upload",
		Duration: int64(time.Second),
		IO:       100,
		Tags: map[string]string{
			ObservedResourceTag:  "s3://checkpoints/prod",
			ObservedDirectionTag: "write",
			ObservedShapeTag:     "upload",
		},
	}}}
	ctx := context.WithValue(context.Background(), keys.PROFILING_CONTEXT_KEY, data)
	ctx = WithObservedThroughput(ctx, cache, "s3://checkpoints/prod", "write")
	RecordObservedThroughput(ctx)

	rate, found := cache.lookup(observedThroughputKey{
		resource:  "s3://checkpoints/prod",
		direction: "write",
		shape:     "upload",
	})
	if !found || rate != 100 {
		t.Fatalf("rate = %d, found = %t", rate, found)
	}
}

func TestThroughputFieldsRoundTrip(t *testing.T) {
	data := &Data{
		IO:                250,
		MaxThroughput:     100,
		MinDuration:       (2500 * time.Millisecond).Nanoseconds(),
		ObservedThroughput: 125,
		ObservedDuration:   (2 * time.Second).Nanoseconds(),
		Tags:              map[string]string{ThroughputSourceTag: "measured"},
	}

	var buffer bytes.Buffer
	if err := Encode(data, &buffer); err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(buffer.String())
	if err != nil {
		t.Fatal(err)
	}
	if decoded.MaxThroughput != data.MaxThroughput || decoded.MinDuration != data.MinDuration || decoded.ObservedThroughput != data.ObservedThroughput || decoded.ObservedDuration != data.ObservedDuration {
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

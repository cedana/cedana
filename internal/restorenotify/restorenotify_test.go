package restorenotify

import (
	"context"
	"encoding/json"
	"testing"
)

type fakePublisher struct {
	queue   string
	payload []byte
}

func (f *fakePublisher) Publish(_ context.Context, queue string, payload []byte) error {
	f.queue = queue
	f.payload = append([]byte(nil), payload...)
	return nil
}

func (f *fakePublisher) Close() error {
	return nil
}

func TestEncodeDecodeEnvRoundTrip(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Enabled:          true,
		RestoreUUID:      "c9bd2efc-36b2-42fe-a6e1-a8fe61ab3f7d",
		NotificationName: "restore-flow",
		RabbitMQURL:      "amqp://guest:guest@localhost:5672/",
		Metadata:         map[string]any{"foo": "bar"},
	}

	item, err := EncodeEnv(cfg)
	if err != nil {
		t.Fatalf("EncodeEnv() error = %v", err)
	}

	decoded, filtered, err := DecodeEnv([]string{"A=B", item, "C=D"})
	if err != nil {
		t.Fatalf("DecodeEnv() error = %v", err)
	}
	if decoded == nil {
		t.Fatal("DecodeEnv() returned nil config")
	}
	if len(filtered) != 2 {
		t.Fatalf("filtered env length = %d, want 2", len(filtered))
	}
	if decoded.RestoreUUID != cfg.RestoreUUID {
		t.Fatalf("RestoreUUID = %q, want %q", decoded.RestoreUUID, cfg.RestoreUUID)
	}
	if decoded.Metadata["foo"] != "bar" {
		t.Fatalf("metadata foo = %#v, want %q", decoded.Metadata["foo"], "bar")
	}
}

func TestProfilingObjectPath(t *testing.T) {
	t.Parallel()

	const restoreID = "c9bd2efc-36b2-42fe-a6e1-a8fe61ab3f7d"

	tests := []struct {
		name        string
		restorePath string
		want        string
	}{
		{
			name:        "local directory",
			restorePath: "/tmp/checkpoints/demo",
			want:        "/tmp/checkpoints/demo/restore-" + restoreID + ".json",
		},
		{
			name:        "local archive",
			restorePath: "/tmp/checkpoints/demo.tar.gz",
			want:        "/tmp/checkpoints/restore-" + restoreID + ".json",
		},
		{
			name:        "remote archive",
			restorePath: "s3://bucket/demo.tar.lz4",
			want:        "s3://bucket/restore-" + restoreID + ".json",
		},
		{
			name:        "remote directory",
			restorePath: "cedana://tenant/checkpoints/demo",
			want:        "cedana://tenant/checkpoints/demo/restore-" + restoreID + ".json",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ProfilingObjectPath(tt.restorePath, restoreID); got != tt.want {
				t.Fatalf("ProfilingObjectPath() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPublishPayloadUsesEventQueueAndMetadata(t *testing.T) {
	t.Parallel()

	fake := &fakePublisher{}
	publish := NewPublishFunc(func(ctx context.Context, url string) (Publisher, error) {
		if url == "" {
			t.Fatal("publisher URL was empty")
		}
		return fake, nil
	})

	cfg := Config{
		Enabled:          true,
		RestoreUUID:      "c9bd2efc-36b2-42fe-a6e1-a8fe61ab3f7d",
		NotificationName: "restore-flow",
		RabbitMQURL:      "amqp://guest:guest@localhost:5672/",
		RestorePath:      "/tmp/demo",
	}

	if err := publish(context.Background(), cfg, EventStart); err != nil {
		t.Fatalf("publish() error = %v", err)
	}
	if fake.queue != "restore_start" {
		t.Fatalf("queue = %q, want restore_start", fake.queue)
	}

	var payload Payload
	if err := json.Unmarshal(fake.payload, &payload); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if payload.Metadata["notification_name"] != "restore-flow" {
		t.Fatalf("notification_name metadata = %#v, want %q", payload.Metadata["notification_name"], "restore-flow")
	}
}

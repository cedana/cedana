package restorenotify

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cedana/cedana/pkg/profiling"
	"github.com/google/uuid"
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

func sampleProfilingData() *profiling.Data {
	return &profiling.Data{
		Name:     "restore",
		Duration: 123,
		IO:       456,
		Components: []*profiling.Data{
			{Name: "criu", Duration: 42},
			{Name: "storage", Duration: 17, IO: 64},
		},
	}
}

func TestParseEventAndQueue(t *testing.T) {
	t.Parallel()

	cases := map[string]Event{
		"start":   EventStart,
		"success": EventSuccess,
		"error":   EventError,
	}

	for input, want := range cases {
		input := input
		want := want

		t.Run(input, func(t *testing.T) {
			t.Parallel()

			got, err := ParseEvent("  " + strings.ToUpper(input) + "  ")
			if err != nil {
				t.Fatalf("ParseEvent() error = %v", err)
			}
			if got != want {
				t.Fatalf("ParseEvent() = %q, want %q", got, want)
			}
			if gotQueue := got.QueueName(); gotQueue != "restore_"+input {
				t.Fatalf("QueueName() = %q, want %q", gotQueue, "restore_"+input)
			}
		})
	}

	if _, err := ParseEvent("bogus"); err == nil {
		t.Fatal("ParseEvent() succeeded for an invalid event")
	}
}

func TestConfigPrepareDefaultsAndMetadata(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Enabled:             true,
		RabbitMQURL:         "amqp://guest:guest@localhost:5672/",
		RestorePath:         "/tmp/checkpoints/demo.tar.gz",
		ProfilingUploadPath: "cedana://cluster-a/profiling",
		NotificationName:    "restore-flow",
		ClusterID:           "cluster-a",
	}

	if err := cfg.Prepare(); err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if cfg.RestoreUUID == "" {
		t.Fatal("Prepare() did not populate RestoreUUID")
	}
	if _, err := uuid.Parse(cfg.RestoreUUID); err != nil {
		t.Fatalf("RestoreUUID is not a valid UUID: %v", err)
	}
	if cfg.StorageProvider != "local" {
		t.Fatalf("StorageProvider = %q, want local", cfg.StorageProvider)
	}
	if cfg.ClusterID != "cluster-a" {
		t.Fatalf("ClusterID = %q, want cluster-a", cfg.ClusterID)
	}
	if got := cfg.Metadata["notification_name"]; got != "restore-flow" {
		t.Fatalf("metadata notification_name = %#v, want %q", got, "restore-flow")
	}
}

func TestConfigPayloadCarriesMetadataAndProfilingState(t *testing.T) {
	t.Parallel()

	cfg := Config{
		RestoreUUID:         "c9bd2efc-36b2-42fe-a6e1-a8fe61ab3f7d",
		PathID:              "path-1",
		RestorePath:         "/tmp/checkpoints/demo",
		ProfilingUploadPath: "cedana://cluster-a/profiling",
		ClusterID:           "cluster-a",
		WorkloadType:        "process",
		CheckpointID:        "checkpoint-1",
		ActionIDs:           []string{"action-1", "action-2"},
		ActionScope:         "job",
		StorageProvider:     "local",
		ErrorMessage:        "restore failed",
		Metadata:            map[string]any{"foo": "bar"},
		RequestMetadata:     map[string]any{"request": "meta"},
		RuntimeMetadata:     map[string]any{"runtime": "meta"},
		ProfilingObject:     "/tmp/checkpoints/demo/restore-c9bd2efc-36b2-42fe-a6e1-a8fe61ab3f7d.json",
		ProfilingError:      "write failed",
		NotificationName:    "restore-flow",
	}

	payload := cfg.Payload(EventError)
	if payload.ErrorMessage != cfg.ErrorMessage {
		t.Fatalf("ErrorMessage = %q, want %q", payload.ErrorMessage, cfg.ErrorMessage)
	}
	if payload.ProfilingPath != cfg.ProfilingObject {
		t.Fatalf("ProfilingPath = %q, want %q", payload.ProfilingPath, cfg.ProfilingObject)
	}
	if payload.Metadata["foo"] != "bar" {
		t.Fatalf("Metadata foo = %#v, want %q", payload.Metadata["foo"], "bar")
	}
	if payload.RequestMetadata["request"] != "meta" {
		t.Fatalf("RequestMetadata request = %#v, want %q", payload.RequestMetadata["request"], "meta")
	}
	if payload.RuntimeMetadata["runtime"] != "meta" {
		t.Fatalf("RuntimeMetadata runtime = %#v, want %q", payload.RuntimeMetadata["runtime"], "meta")
	}
	if payload.RuntimeMetadata["profiling_upload_error"] != "write failed" {
		t.Fatalf("profiling_upload_error = %#v, want %q", payload.RuntimeMetadata["profiling_upload_error"], "write failed")
	}
	if len(payload.ActionIDs) != 2 || payload.ActionIDs[0] != "action-1" {
		t.Fatalf("ActionIDs = %#v, want copied action IDs", payload.ActionIDs)
	}
}

func TestEncodeDecodeRestoreNotificationConfigIncludesProfilingUploadPath(t *testing.T) {
	t.Parallel()

	cfg := Config{
		RestoreUUID:         "c9bd2efc-36b2-42fe-a6e1-a8fe61ab3f7d",
		RestorePath:         "/tmp/checkpoints/demo",
		ProfilingUploadPath: "cedana://cluster-a/profiling/custom.json",
		UploadProfiling:     true,
		RequestMetadata:     map[string]any{"request": "meta"},
		RuntimeMetadata:     map[string]any{"runtime": "meta"},
	}

	env, err := EncodeEnv(cfg)
	if err != nil {
		t.Fatalf("EncodeEnv() error = %v", err)
	}

	decoded, filtered, err := DecodeEnv([]string{"FOO=bar", env})
	if err != nil {
		t.Fatalf("DecodeEnv() error = %v", err)
	}
	if len(filtered) != 1 || filtered[0] != "FOO=bar" {
		t.Fatalf("filtered env = %#v, want preserved non-restore env", filtered)
	}
	if decoded.ProfilingUploadPath != cfg.ProfilingUploadPath {
		t.Fatalf("ProfilingUploadPath = %q, want %q", decoded.ProfilingUploadPath, cfg.ProfilingUploadPath)
	}
	if !decoded.UploadProfiling {
		t.Fatal("UploadProfiling was not preserved")
	}
}

func TestWriteAndUploadProfilingJSON(t *testing.T) {
	t.Parallel()

	data := sampleProfilingData()
	dir := t.TempDir()

	writePath := filepath.Join(dir, "profiling.json")
	if err := WriteProfilingJSON(writePath, data); err != nil {
		t.Fatalf("WriteProfilingJSON() error = %v", err)
	}

	body, err := os.ReadFile(writePath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !strings.Contains(string(body), "\"name\": \"restore\"") {
		t.Fatalf("written profiling JSON = %s", string(body))
	}

	const restoreID = "c9bd2efc-36b2-42fe-a6e1-a8fe61ab3f7d"
	target := filepath.Join(dir, "profile.json")
	wantDefault := filepath.Join(dir, "restore-"+restoreID+".json")
	if got := ProfilingObjectPath(dir, restoreID); got != wantDefault {
		t.Fatalf("ProfilingObjectPath() = %q, want %q", got, wantDefault)
	}
	objectPath, err := UploadProfilingJSON(context.Background(), target, data)
	if err != nil {
		t.Fatalf("UploadProfilingJSON() error = %v", err)
	}

	wantPath := target
	if objectPath != wantPath {
		t.Fatalf("UploadProfilingJSON() path = %q, want %q", objectPath, wantPath)
	}

	body, err = os.ReadFile(objectPath)
	if err != nil {
		t.Fatalf("ReadFile(uploaded) error = %v", err)
	}

	var uploaded profiling.Data
	if err := json.Unmarshal(body, &uploaded); err != nil {
		t.Fatalf("Unmarshal(uploaded) error = %v", err)
	}
	if uploaded.Name != data.Name || uploaded.Duration != data.Duration {
		t.Fatalf("uploaded profiling JSON = %#v, want %#v", uploaded, data)
	}
}

func TestDispatcherProfilesUploadAndWrite(t *testing.T) {
	t.Parallel()

	dispatcher := NewDispatcher(func(fn func()) { go fn() })

	dir := t.TempDir()
	restoreDir := filepath.Join(dir, "restore")
	if err := os.MkdirAll(restoreDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	const restoreID = "c9bd2efc-36b2-42fe-a6e1-a8fe61ab3f7d"
	data := sampleProfilingData()
	writePath := filepath.Join(dir, "profiling.json")
	cfg := &Config{
		RestorePath:     restoreDir,
		RestoreUUID:     restoreID,
		UploadProfiling: true,
	}

	dispatcher.SubmitProfilingWrite(writePath, data)
	dispatcher.SubmitProfilingUpload(context.Background(), cfg, data)
	dispatcher.Close()
	dispatcher.Wait()

	if _, err := os.Stat(writePath); err != nil {
		t.Fatalf("profiling write missing: %v", err)
	}

	wantObject := filepath.Join(restoreDir, "restore-"+restoreID+".json")
	if cfg.ProfilingObject != wantObject {
		t.Fatalf("ProfilingObject = %q, want %q", cfg.ProfilingObject, wantObject)
	}
	if _, err := os.Stat(cfg.ProfilingObject); err != nil {
		t.Fatalf("profiling upload missing: %v", err)
	}
}

func TestDispatcherUsesExplicitProfilingUploadPath(t *testing.T) {
	t.Parallel()

	dispatcher := NewDispatcher(func(fn func()) { go fn() })

	dir := t.TempDir()
	target := filepath.Join(dir, "profiling.json")
	data := sampleProfilingData()
	cfg := &Config{
		RestorePath:         filepath.Join(dir, "restore"),
		RestoreUUID:         "c9bd2efc-36b2-42fe-a6e1-a8fe61ab3f7d",
		ProfilingUploadPath: target,
		UploadProfiling:     true,
	}

	dispatcher.SubmitProfilingUpload(context.Background(), cfg, data)
	dispatcher.Close()
	dispatcher.Wait()

	if cfg.ProfilingObject != target {
		t.Fatalf("ProfilingObject = %q, want %q", cfg.ProfilingObject, target)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("custom profiling upload missing: %v", err)
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
		ProfilingObject:  "/tmp/demo/restore-c9bd2efc-36b2-42fe-a6e1-a8fe61ab3f7d.json",
		ProfilingError:   "upload failed",
		RuntimeMetadata:  map[string]any{"runtime": "meta"},
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
	if payload.ProfilingPath != cfg.ProfilingObject {
		t.Fatalf("ProfilingPath = %q, want %q", payload.ProfilingPath, cfg.ProfilingObject)
	}
	if payload.RuntimeMetadata["profiling_upload_error"] != "upload failed" {
		t.Fatalf("profiling_upload_error = %#v, want %q", payload.RuntimeMetadata["profiling_upload_error"], "upload failed")
	}
}

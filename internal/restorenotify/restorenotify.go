package restorenotify

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	pathpkg "path"
	"path/filepath"
	"strings"
	"time"

	"github.com/cedana/cedana/internal/cedana/filesystem"
	"github.com/cedana/cedana/pkg/config"
	"github.com/cedana/cedana/pkg/features"
	cedanaio "github.com/cedana/cedana/pkg/io"
	"github.com/cedana/cedana/pkg/profiling"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

const envKey = "CEDANA_INTERNAL_RESTORE_NOTIFICATION"

const (
	asyncTaskBuffer      = 8
	publishTimeout       = 15 * time.Second
	profilingTaskTimeout = 2 * time.Minute
)

type Event string

const (
	EventStart   Event = "start"
	EventSuccess Event = "success"
	EventError   Event = "error"
)

type Config struct {
	Enabled            bool           `json:"enabled"`
	Event              Event          `json:"event,omitempty"`
	RestoreUUID        string         `json:"restore_uuid,omitempty"`
	NotificationName   string         `json:"notification_name,omitempty"`
	Router             string         `json:"router,omitempty"`
	RabbitMQURL        string         `json:"rabbitmq_url,omitempty"`
	ClusterID          string         `json:"cluster_id,omitempty"`
	WorkloadType       string         `json:"workload_type,omitempty"`
	CheckpointID       string         `json:"checkpoint_id,omitempty"`
	CheckpointActionID string         `json:"checkpoint_action_id,omitempty"`
	ActionIDs          []string       `json:"action_ids,omitempty"`
	ActionScope        string         `json:"action_scope,omitempty"`
	PathID             string         `json:"path_id,omitempty"`
	RestorePath        string         `json:"restore_path,omitempty"`
	StorageProvider    string         `json:"storage_provider,omitempty"`
	ErrorMessage       string         `json:"error_message,omitempty"`
	Metadata           map[string]any `json:"metadata,omitempty"`
	RequestMetadata    map[string]any `json:"request_metadata,omitempty"`
	RuntimeMetadata    map[string]any `json:"runtime_metadata,omitempty"`
	ProfilingPath      string         `json:"profiling_path,omitempty"`
	UploadProfiling    bool           `json:"upload_profiling,omitempty"`
	ProfilingObject    string         `json:"profiling_object,omitempty"`
	ProfilingError     string         `json:"profiling_error,omitempty"`
}

type Payload struct {
	RestoreUUID        string         `json:"restore_uuid"`
	PathID             string         `json:"path_id,omitempty"`
	RestorePath        string         `json:"restore_path,omitempty"`
	ClusterID          string         `json:"cluster_id,omitempty"`
	WorkloadType       string         `json:"workload_type,omitempty"`
	CheckpointID       string         `json:"checkpoint_id,omitempty"`
	CheckpointActionID string         `json:"checkpoint_action_id,omitempty"`
	ActionIDs          []string       `json:"action_ids,omitempty"`
	ActionScope        string         `json:"action_scope,omitempty"`
	StorageProvider    string         `json:"storage_provider,omitempty"`
	ErrorMessage       string         `json:"error_message,omitempty"`
	ProfilingPath      string         `json:"profiling_path,omitempty"`
	Metadata           map[string]any `json:"metadata,omitempty"`
	RequestMetadata    map[string]any `json:"request_metadata,omitempty"`
	RuntimeMetadata    map[string]any `json:"runtime_metadata,omitempty"`
}

func (e Event) QueueName() string {
	switch e {
	case EventStart:
		return "restore_start"
	case EventSuccess:
		return "restore_success"
	case EventError:
		return "restore_error"
	default:
		return ""
	}
}

func ParseEvent(value string) (Event, error) {
	event := Event(strings.ToLower(strings.TrimSpace(value)))
	switch event {
	case EventStart, EventSuccess, EventError:
		return event, nil
	default:
		return "", fmt.Errorf("invalid restore event %q", value)
	}
}

func (c *Config) Prepare() error {
	if c.RestoreUUID == "" {
		c.RestoreUUID = uuid.NewString()
	}
	if _, err := uuid.Parse(c.RestoreUUID); err != nil {
		return fmt.Errorf("invalid restore UUID: %w", err)
	}
	if c.ClusterID == "" {
		c.ClusterID = config.Global.ClusterID
	}
	if c.StorageProvider == "" {
		c.StorageProvider = StorageProviderFromPath(c.RestorePath)
	}
	if c.Metadata == nil {
		c.Metadata = map[string]any{}
	}
	if c.NotificationName != "" {
		if _, ok := c.Metadata["notification_name"]; !ok {
			c.Metadata["notification_name"] = c.NotificationName
		}
	}
	if c.Enabled && c.RabbitMQURL == "" {
		return fmt.Errorf("rabbitmq URL is required when restore notifications are enabled")
	}

	return nil
}

func (c Config) Queue(event Event) string {
	if c.Router != "" {
		return c.Router
	}
	return event.QueueName()
}

func (c Config) Payload(event Event) Payload {
	payload := Payload{
		RestoreUUID:        c.RestoreUUID,
		PathID:             c.PathID,
		RestorePath:        c.RestorePath,
		ClusterID:          c.ClusterID,
		WorkloadType:       c.WorkloadType,
		CheckpointID:       c.CheckpointID,
		CheckpointActionID: c.CheckpointActionID,
		ActionIDs:          append([]string(nil), c.ActionIDs...),
		ActionScope:        c.ActionScope,
		StorageProvider:    c.StorageProvider,
		Metadata:           cloneMap(c.Metadata),
		RequestMetadata:    cloneMap(c.RequestMetadata),
		RuntimeMetadata:    cloneMap(c.RuntimeMetadata),
	}
	if event == EventError {
		payload.ErrorMessage = c.ErrorMessage
	}
	if c.ProfilingObject != "" {
		payload.ProfilingPath = c.ProfilingObject
	}
	if c.ProfilingError != "" {
		if payload.RuntimeMetadata == nil {
			payload.RuntimeMetadata = map[string]any{}
		}
		payload.RuntimeMetadata["profiling_upload_error"] = c.ProfilingError
	}

	return payload
}

type Publisher interface {
	Publish(ctx context.Context, queue string, payload []byte) error
	Close() error
}

type PublishFunc func(ctx context.Context, cfg Config, event Event) error

func NewPublishFunc(factory func(ctx context.Context, url string) (Publisher, error)) PublishFunc {
	return func(ctx context.Context, cfg Config, event Event) error {
		if err := cfg.Prepare(); err != nil {
			return err
		}

		publisher, err := factory(ctx, cfg.RabbitMQURL)
		if err != nil {
			return err
		}
		defer publisher.Close()

		body, err := json.Marshal(cfg.Payload(event))
		if err != nil {
			return fmt.Errorf("marshal restore event payload: %w", err)
		}

		if err := publisher.Publish(ctx, cfg.Queue(event), body); err != nil {
			return fmt.Errorf("publish %s restore event: %w", event, err)
		}
		return nil
	}
}

func EncodeEnv(cfg Config) (string, error) {
	body, err := json.Marshal(cfg)
	if err != nil {
		return "", fmt.Errorf("marshal restore notification config: %w", err)
	}
	return envKey + "=" + base64.StdEncoding.EncodeToString(body), nil
}

func DecodeEnv(env []string) (*Config, []string, error) {
	filtered := make([]string, 0, len(env))
	var cfg *Config
	for _, item := range env {
		if !strings.HasPrefix(item, envKey+"=") {
			filtered = append(filtered, item)
			continue
		}

		encoded := strings.TrimPrefix(item, envKey+"=")
		body, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return nil, nil, fmt.Errorf("decode restore notification config: %w", err)
		}

		decoded := &Config{}
		if err := json.Unmarshal(body, decoded); err != nil {
			return nil, nil, fmt.Errorf("unmarshal restore notification config: %w", err)
		}
		cfg = decoded
	}
	return cfg, filtered, nil
}

func WriteProfilingJSON(target string, data *profiling.Data) error {
	if data == nil {
		return nil
	}
	body, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal profiling data: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("create profiling output directory: %w", err)
	}
	if err := os.WriteFile(target, body, 0o644); err != nil {
		return fmt.Errorf("write profiling output: %w", err)
	}
	return nil
}

func UploadProfilingJSON(ctx context.Context, restorePath, restoreUUID string, data *profiling.Data) (string, error) {
	if data == nil {
		return "", nil
	}
	if restorePath == "" {
		return "", fmt.Errorf("restore path is required to upload profiling data")
	}

	storage, err := ResolveStorage(ctx, restorePath)
	if err != nil {
		return "", err
	}

	target := ProfilingObjectPath(restorePath, restoreUUID)
	body, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal profiling data: %w", err)
	}

	writer, err := storage.Create(ctx, target)
	if err != nil {
		return "", fmt.Errorf("create profiling object: %w", err)
	}
	defer writer.Close()

	if _, err := writer.Write(body); err != nil {
		return "", fmt.Errorf("write profiling object: %w", err)
	}

	return target, nil
}

func ResolveStorage(ctx context.Context, target string) (cedanaio.Storage, error) {
	var storage cedanaio.Storage = &filesystem.Storage{}
	if !strings.Contains(target, "://") {
		return storage, nil
	}

	pluginName := fmt.Sprintf("storage/%s", strings.SplitN(target, "://", 2)[0])
	if err := features.Storage.IfAvailable(func(name string, newPluginStorage func(ctx context.Context) (cedanaio.Storage, error)) error {
		var err error
		storage, err = newPluginStorage(ctx)
		return err
	}, pluginName); err != nil {
		return nil, fmt.Errorf("resolve storage plugin %q: %w", pluginName, err)
	}

	return storage, nil
}

func ProfilingObjectPath(restorePath, restoreUUID string) string {
	base := restorePath
	if LooksLikeArchivePath(restorePath) {
		base = StorageDir(restorePath)
	}
	filename := "restore-" + restoreUUID + ".json"
	return JoinStoragePath(base, filename)
}

func LooksLikeArchivePath(target string) bool {
	clean := target
	if strings.Contains(target, "://") {
		clean = strings.SplitN(target, "://", 2)[1]
	}
	switch strings.ToLower(pathpkg.Ext(clean)) {
	case ".tar", ".gz", ".gzip", ".lz4", ".zlib":
		return true
	default:
		return false
	}
}

func JoinStoragePath(base, name string) string {
	if base == "" {
		return name
	}
	if strings.Contains(base, "://") {
		parts := strings.SplitN(base, "://", 2)
		suffix := strings.TrimSuffix(parts[1], "/")
		if suffix == "" {
			return parts[0] + "://" + name
		}
		return parts[0] + "://" + pathpkg.Join(suffix, name)
	}
	return filepath.Join(base, name)
}

func StorageDir(target string) string {
	if strings.Contains(target, "://") {
		parts := strings.SplitN(target, "://", 2)
		dir := pathpkg.Dir(parts[1])
		if dir == "." || dir == "/" {
			return parts[0] + "://"
		}
		return parts[0] + "://" + dir
	}
	return filepath.Dir(target)
}

func StorageProviderFromPath(target string) string {
	if target == "" {
		return ""
	}
	if strings.Contains(target, "://") {
		return strings.SplitN(target, "://", 2)[0]
	}
	return "local"
}

func cloneMap(src map[string]any) map[string]any {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]any, len(src))
	for key, value := range src {
		dst[key] = value
	}
	return dst
}

func LogPublishFailure(cfg Config, event Event, err error) {
	log.Error().
		Err(err).
		Str("event", string(event)).
		Str("restore_uuid", cfg.RestoreUUID).
		Msg("restore notification publish failed")
}

type Dispatcher struct {
	tasks chan func()
}

func NewDispatcher(launch func(func())) *Dispatcher {
	d := &Dispatcher{
		tasks: make(chan func(), asyncTaskBuffer),
	}
	launch(func() {
		for task := range d.tasks {
			task()
		}
	})
	return d
}

func (d *Dispatcher) Submit(task func()) {
	if d == nil || task == nil {
		return
	}
	d.tasks <- task
}

func (d *Dispatcher) Close() {
	if d == nil {
		return
	}
	close(d.tasks)
}

func (d *Dispatcher) SubmitPublish(ctx context.Context, cfg Config, event Event) {
	cfgCopy := cfg
	d.Submit(func() {
		taskCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), publishTimeout)
		defer cancel()
		if err := NewPublishFunc(NewRabbitPublisher)(taskCtx, cfgCopy, event); err != nil {
			LogPublishFailure(cfgCopy, event, err)
		}
	})
}

func (d *Dispatcher) SubmitProfilingWrite(path string, data *profiling.Data) {
	if path == "" || data == nil {
		return
	}
	d.Submit(func() {
		if err := WriteProfilingJSON(path, data); err != nil {
			log.Error().Err(err).Str("profiling_path", path).Msg("restore profiling JSON write failed")
		}
	})
}

func (d *Dispatcher) SubmitProfilingUpload(ctx context.Context, cfg *Config, data *profiling.Data) {
	if cfg == nil || !cfg.UploadProfiling || data == nil {
		return
	}
	cfgCopy := *cfg
	d.Submit(func() {
		taskCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), profilingTaskTimeout)
		defer cancel()

		objectPath, err := UploadProfilingJSON(taskCtx, cfgCopy.RestorePath, cfgCopy.RestoreUUID, data)
		if err != nil {
			log.Error().Err(err).Str("restore_uuid", cfgCopy.RestoreUUID).Msg("restore profiling upload failed")
			cfg.ProfilingError = err.Error()
			return
		}

		cfg.ProfilingObject = objectPath
	})
}

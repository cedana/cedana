package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cedana/cedana/internal/restorenotify"
	"github.com/cedana/cedana/pkg/flags"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func init() {
	rootCmd.AddCommand(notifyCmd)
	notifyCmd.AddCommand(notifyRestoreCmd)

	addRestoreNotificationFlags(notifyRestoreCmd.Flags(), false)
	notifyRestoreCmd.Flags().String(flags.EventFlag.Full, "", "restore event to publish: start, success, or error")
	notifyRestoreCmd.MarkFlagRequired(flags.EventFlag.Full)
	notifyRestoreCmd.MarkFlagRequired(flags.RestoreIDFlag.Full)
	notifyRestoreCmd.MarkFlagRequired(flags.NotificationNameFlag.Full)
	notifyRestoreCmd.MarkFlagRequired(flags.RabbitMQURLFlag.Full)
}

var notifyCmd = &cobra.Command{
	Use:   "notify",
	Short: "Publish lifecycle notifications",
}

var notifyRestoreCmd = &cobra.Command{
	Use:   "restore",
	Short: "Publish a restore lifecycle notification",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := restoreNotificationConfigFromFlags(cmd.Flags(), true)
		if err != nil {
			return err
		}

		event, err := restorenotify.ParseEvent(mustGetString(cmd.Flags(), flags.EventFlag.Full))
		if err != nil {
			return err
		}
		cfg.Event = event

		publish := restorenotify.NewPublishFunc(restorenotify.NewRabbitPublisher)
		return publish(cmd.Context(), cfg, event)
	},
}

func addRestoreNotificationFlags(set *pflag.FlagSet, includeEnable bool) {
	if includeEnable {
		set.Bool(flags.NotifyFlag.Full, false, "publish restore lifecycle notifications")
	}
	set.String(flags.RestoreIDFlag.Full, "", "restore UUID used in restore lifecycle events")
	set.String(flags.NotificationNameFlag.Full, "", "notification name carried in restore event metadata")
	set.String(flags.RouterFlag.Full, "", "override the destination queue; defaults to the restore event queue")
	set.String(flags.RabbitMQURLFlag.Full, "", "RabbitMQ connection URL used for direct restore event publishing")
	set.String(flags.ClusterIDFlag.Full, "", "cluster identifier to include in restore event payloads")
	set.String(flags.WorkloadTypeFlag.Full, "", "workload type to include in restore event payloads")
	set.String(flags.CheckpointIDFlag.Full, "", "checkpoint identifier linked to the restore")
	set.String(flags.CheckpointActionIDFlag.Full, "", "checkpoint action identifier linked to the restore")
	set.StringSlice(flags.ActionIDFlag.Full, nil, "action identifier to link to the restore event; can be repeated")
	set.String(flags.ActionScopeFlag.Full, "", "action scope to include in restore event payloads")
	set.String(flags.PathIDFlag.Full, "", "path identifier to include in restore event payloads")
	set.String(flags.RestorePathFlag.Full, "", "restore path to include in restore event payloads")
	set.String(flags.StorageProviderFlag.Full, "", "storage provider to include in restore event payloads")
	set.String(flags.ErrorMessageFlag.Full, "", "error message for restore error events")
	set.String(flags.MetadataFlag.Full, "", "JSON object for generic event metadata")
	set.String(flags.RequestMetadataFlag.Full, "", "JSON object for request metadata")
	set.String(flags.RuntimeMetadataFlag.Full, "", "JSON object for runtime metadata")
	set.String(flags.ProfilingPathFlag.Full, "", "write restore profiling JSON to this local file path")
	set.Bool(flags.UploadProfilingFlag.Full, false, "upload restore profiling JSON next to the restore path using the selected storage backend")
}

func restoreNotificationConfigFromFlags(set *pflag.FlagSet, force bool) (restorenotify.Config, error) {
	enabled := force || mustGetBool(set, flags.NotifyFlag.Full)
	cfg := restorenotify.Config{
		Enabled:            enabled,
		RestoreUUID:        mustGetString(set, flags.RestoreIDFlag.Full),
		NotificationName:   mustGetString(set, flags.NotificationNameFlag.Full),
		Router:             mustGetString(set, flags.RouterFlag.Full),
		RabbitMQURL:        mustGetString(set, flags.RabbitMQURLFlag.Full),
		ClusterID:          mustGetString(set, flags.ClusterIDFlag.Full),
		WorkloadType:       mustGetString(set, flags.WorkloadTypeFlag.Full),
		CheckpointID:       mustGetString(set, flags.CheckpointIDFlag.Full),
		CheckpointActionID: mustGetString(set, flags.CheckpointActionIDFlag.Full),
		ActionIDs:          mustGetStringSlice(set, flags.ActionIDFlag.Full),
		ActionScope:        mustGetString(set, flags.ActionScopeFlag.Full),
		PathID:             mustGetString(set, flags.PathIDFlag.Full),
		RestorePath:        mustGetString(set, flags.RestorePathFlag.Full),
		StorageProvider:    mustGetString(set, flags.StorageProviderFlag.Full),
		ErrorMessage:       mustGetString(set, flags.ErrorMessageFlag.Full),
		ProfilingPath:      mustGetString(set, flags.ProfilingPathFlag.Full),
		UploadProfiling:    mustGetBool(set, flags.UploadProfilingFlag.Full),
	}

	metadata, err := jsonObjectFlag(set, flags.MetadataFlag.Full)
	if err != nil {
		return cfg, err
	}
	requestMetadata, err := jsonObjectFlag(set, flags.RequestMetadataFlag.Full)
	if err != nil {
		return cfg, err
	}
	runtimeMetadata, err := jsonObjectFlag(set, flags.RuntimeMetadataFlag.Full)
	if err != nil {
		return cfg, err
	}
	cfg.Metadata = metadata
	cfg.RequestMetadata = requestMetadata
	cfg.RuntimeMetadata = runtimeMetadata

	if enabled {
		if err := cfg.Prepare(); err != nil {
			return cfg, err
		}
	}

	return cfg, nil
}

func jsonObjectFlag(set *pflag.FlagSet, name string) (map[string]any, error) {
	value := strings.TrimSpace(mustGetString(set, name))
	if value == "" {
		return nil, nil
	}
	var object map[string]any
	if err := json.Unmarshal([]byte(value), &object); err != nil {
		return nil, fmt.Errorf("invalid JSON for --%s: %w", name, err)
	}
	return object, nil
}

func publishRestoreEvent(ctx context.Context, cfg restorenotify.Config, event restorenotify.Event) error {
	if !cfg.Enabled {
		return nil
	}
	return restorenotify.NewPublishFunc(restorenotify.NewRabbitPublisher)(ctx, cfg, event)
}

func mustGetString(set *pflag.FlagSet, name string) string {
	value, _ := set.GetString(name)
	return value
}

func mustGetBool(set *pflag.FlagSet, name string) bool {
	value, _ := set.GetBool(name)
	return value
}

func mustGetStringSlice(set *pflag.FlagSet, name string) []string {
	value, _ := set.GetStringSlice(name)
	return value
}

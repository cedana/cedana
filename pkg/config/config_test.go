package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
)

func TestInitPersistsDefaultAWSCredentialsMode(t *testing.T) {
	originalGlobal := Global
	originalDir := Dir
	t.Cleanup(func() {
		viper.Reset()
		Global = originalGlobal
		Dir = originalDir
		setDefaults()
		bindEnvVars()
	})

	viper.Reset()
	Global = originalGlobal
	Global.AWS.CredentialsMode = DEFAULT_AWS_CREDENTIALS_MODE
	setDefaults()
	bindEnvVars()

	configDir := t.TempDir()
	if err := Init(Args{ConfigDir: configDir}); err != nil {
		t.Fatalf("init config: %v", err)
	}
	if Global.AWS.CredentialsMode != DEFAULT_AWS_CREDENTIALS_MODE {
		t.Fatalf("credentials mode = %q, want %q", Global.AWS.CredentialsMode, DEFAULT_AWS_CREDENTIALS_MODE)
	}

	data, err := os.ReadFile(filepath.Join(configDir, FILE_NAME+"."+FILE_TYPE))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var persisted map[string]any
	if err := json.Unmarshal(data, &persisted); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	aws, ok := persisted["aws"].(map[string]any)
	if !ok {
		t.Fatalf("persisted AWS config has type %T", persisted["aws"])
	}
	if got := aws["credentials_mode"]; got != DEFAULT_AWS_CREDENTIALS_MODE {
		t.Fatalf("persisted credentials mode = %v, want %q", got, DEFAULT_AWS_CREDENTIALS_MODE)
	}
}

func TestInitPersistsExplicitAWSCredentialsMode(t *testing.T) {
	originalGlobal := Global
	originalDir := Dir
	t.Cleanup(func() {
		viper.Reset()
		Global = originalGlobal
		Dir = originalDir
		setDefaults()
		bindEnvVars()
	})

	viper.Reset()
	Global = originalGlobal
	Global.AWS.CredentialsMode = DEFAULT_AWS_CREDENTIALS_MODE
	setDefaults()
	bindEnvVars()

	configDir := t.TempDir()
	if err := Init(Args{
		ConfigDir: configDir,
		Config:    `{"aws":{"credentials_mode":"ambient"}}`,
	}); err != nil {
		t.Fatalf("init config: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(configDir, FILE_NAME+"."+FILE_TYPE))
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var persisted map[string]any
	if err := json.Unmarshal(data, &persisted); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	aws, ok := persisted["aws"].(map[string]any)
	if !ok {
		t.Fatalf("persisted AWS config has type %T", persisted["aws"])
	}
	if got := aws["credentials_mode"]; got != "ambient" {
		t.Fatalf("persisted credentials mode = %v, want ambient", got)
	}
}

// A config written by a newer version (with fields this one doesn't know) must still load,
// as daemons and their configs are upgraded separately, e.g. helm upgrades on a node
func TestLoadIgnoresUnknownFields(t *testing.T) {
	originalGlobal := Global
	originalDir := Dir
	t.Cleanup(func() {
		viper.Reset()
		Global = originalGlobal
		Dir = originalDir
		setDefaults()
		bindEnvVars()
	})

	viper.Reset()
	setDefaults()
	bindEnvVars()

	configDir := t.TempDir()
	newer := `{"address": "0.0.0.0:9999", "added_in_a_newer_version": true, "checkpoint": {"also_new": 1}}`
	if err := os.WriteFile(filepath.Join(configDir, FILE_NAME+"."+FILE_TYPE), []byte(newer), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Load(Args{ConfigDir: configDir}); err != nil {
		t.Fatalf("a config with unknown fields failed to load: %v", err)
	}
	if Global.Address != "0.0.0.0:9999" {
		t.Errorf("address is %q, want the config's %q", Global.Address, "0.0.0.0:9999")
	}
}

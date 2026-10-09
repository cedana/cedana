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

func TestChecksumVerifyDefaultsToWarnAndReadsTheEnvironment(t *testing.T) {
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
	setDefaults()
	bindEnvVars()
	if err := Init(Args{ConfigDir: t.TempDir()}); err != nil {
		t.Fatalf("init config: %v", err)
	}
	if Global.Checkpoint.ChecksumVerify != CHECKSUM_VERIFY_WARN {
		t.Fatalf("default checksum_verify = %q, want %q", Global.Checkpoint.ChecksumVerify, CHECKSUM_VERIFY_WARN)
	}

	t.Setenv("CEDANA_CHECKPOINT_CHECKSUM_VERIFY", CHECKSUM_VERIFY_STRICT)
	viper.Reset()
	Global = originalGlobal
	setDefaults()
	bindEnvVars()
	if err := Init(Args{ConfigDir: t.TempDir()}); err != nil {
		t.Fatalf("init config: %v", err)
	}
	if Global.Checkpoint.ChecksumVerify != CHECKSUM_VERIFY_STRICT {
		t.Fatalf("checksum_verify from the environment = %q, want %q", Global.Checkpoint.ChecksumVerify, CHECKSUM_VERIFY_STRICT)
	}
}

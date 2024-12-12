package config

import "github.com/spf13/viper"

// Contains all config defaults

type ConfigItem[T any] struct {
	Key     string
	Default T
	Env     string
	Get     func(string) T
}

var (
	PORT             = ConfigItem[uint32]{"options.port", 8080, "CEDANA_PORT", viper.GetUint32}
	HOST             = ConfigItem[string]{"options.host", "0.0.0.0", "CEDANA_HOST", viper.GetString}
	USE_VSOCK        = ConfigItem[bool]{"options.use_vsock", false, "CEDANA_USE_VSOCK", viper.GetBool}
	VSOCK_CONTEXT_ID = ConfigItem[uint32]{"options.context_id", 0, "CEDANA_VSOCK_CONTEXT_ID", viper.GetUint32}
	LOG_LEVEL        = ConfigItem[string]{"options.log_level", "info", "CEDANA_LOG_LEVEL", viper.GetString}

	STORAGE_REMOTE   = ConfigItem[bool]{"storage.remote", false, "CEDANA_REMOTE", viper.GetBool}
	STORAGE_DUMP_DIR = ConfigItem[string]{
		"storage.dump_dir",
		"/tmp",
		"CEDANA_STORAGE_DUMP_DIR",
		viper.GetString,
	}
	STORAGE_COMPRESSION = ConfigItem[string]{
		"storage.compression",
		"none",
		"CEDANA_STORAGE_COMPRESSION",
		viper.GetString,
	}

	CEDANA_URL = ConfigItem[string]{
		"connection.cedana_url",
		"unset",
		"CEDANA_URL",
		viper.GetString,
	}
	CEDANA_AUTH_TOKEN = ConfigItem[string]{
		"connection.cedana_auth_token",
		"unset",
		"CEDANA_AUTH_TOKEN",
		viper.GetString,
	}

	CLI_WAIT_FOR_READY = ConfigItem[bool]{
		"cli.wait_for_ready",
		false,
		"CEDANA_CLI_WAIT_FOR_READY",
		viper.GetBool,
	}

	METRICS_ASR = ConfigItem[bool]{
		"metrics.asr",
		false,
		"CEDANA_METRICS_ASR",
		viper.GetBool,
	}
	METRICS_OTEL_ENABLED = ConfigItem[bool]{
		"metrics.otel.enabled",
		false,
		"CEDANA_METRICS_OTEL_ENABLED",
		viper.GetBool,
	}
	METRICS_OTEL_PORT = ConfigItem[int]{
		"metrics.otel.port",
		7777,
		"CEDANA_METRICS_OTEL_PORT",
		viper.GetInt,
	}

	CRIU_LEAVE_RUNNING = ConfigItem[bool]{
		"criu.leave_running",
		false,
		"CEDANA_CRIU_LEAVE_RUNNING",
		viper.GetBool,
	}
	CRIU_BINARY_PATH = ConfigItem[string]{
		"criu.binary_path",
		"criu",
		"CEDANA_CRIU_BINARY_PATH",
		viper.GetString,
	}

	GPU_POOL_SIZE = ConfigItem[int]{
		"gpu.pool_size",
		0,
		"CEDANA_GPU_POOL_SIZE",
		viper.GetInt,
	}
)

func init() {
	setDefaults()
	bindEnvVars()
}

// Set defaults that are used when no value is found in config/env vars
func setDefaults() {
	viper.SetDefault(PORT.Key, PORT.Default)
	viper.SetDefault(HOST.Key, HOST.Default)
	viper.SetDefault(USE_VSOCK.Key, USE_VSOCK.Default)

	viper.SetDefault(STORAGE_REMOTE.Key, STORAGE_REMOTE.Default)
	viper.SetDefault(STORAGE_DUMP_DIR.Key, STORAGE_DUMP_DIR.Default)
	viper.SetDefault(STORAGE_COMPRESSION.Key, STORAGE_COMPRESSION.Default)

	viper.SetDefault(CEDANA_URL.Key, CEDANA_URL.Default)
	viper.SetDefault(CEDANA_AUTH_TOKEN.Key, CEDANA_AUTH_TOKEN.Default)

	viper.SetDefault(CLI_WAIT_FOR_READY.Key, CLI_WAIT_FOR_READY.Default)

	viper.SetDefault(METRICS_ASR.Key, METRICS_ASR.Default)
	viper.SetDefault(METRICS_OTEL_ENABLED.Key, METRICS_OTEL_ENABLED.Default)
	viper.SetDefault(METRICS_OTEL_PORT.Key, METRICS_OTEL_PORT.Default)

	viper.SetDefault(CRIU_LEAVE_RUNNING.Key, CRIU_LEAVE_RUNNING.Default)
	viper.SetDefault(CRIU_BINARY_PATH.Key, CRIU_BINARY_PATH.Default)

	viper.SetDefault(GPU_POOL_SIZE.Key, GPU_POOL_SIZE.Default)
}

// Add bindings for env vars so env vars can be used as backup
// when a value is not found in config when using viper.Get~()
func bindEnvVars() {
	// Related to the config file
	viper.BindEnv(PORT.Key, PORT.Env)
	viper.BindEnv(HOST.Key, HOST.Env)
	viper.BindEnv(USE_VSOCK.Key, USE_VSOCK.Env)

	viper.BindEnv(STORAGE_REMOTE.Key, STORAGE_REMOTE.Env)
	viper.BindEnv(STORAGE_DUMP_DIR.Key, STORAGE_DUMP_DIR.Env)
	viper.BindEnv(STORAGE_COMPRESSION.Key, STORAGE_COMPRESSION.Env)

	viper.BindEnv(CEDANA_URL.Key, CEDANA_URL.Env)
	viper.BindEnv(CEDANA_AUTH_TOKEN.Key, CEDANA_AUTH_TOKEN.Env)

	viper.BindEnv(CLI_WAIT_FOR_READY.Key, CLI_WAIT_FOR_READY.Env)

	viper.BindEnv(METRICS_ASR.Key, METRICS_ASR.Env)
	viper.BindEnv(METRICS_OTEL_ENABLED.Key, METRICS_OTEL_ENABLED.Env)
	viper.BindEnv(METRICS_OTEL_PORT.Key, METRICS_OTEL_PORT.Env)

	viper.BindEnv(CRIU_LEAVE_RUNNING.Key, CRIU_LEAVE_RUNNING.Env)
	viper.BindEnv(CRIU_BINARY_PATH.Key, CRIU_BINARY_PATH.Env)

	viper.BindEnv(GPU_POOL_SIZE.Key, GPU_POOL_SIZE.Env)

	// Env only
	viper.BindEnv(LOG_LEVEL.Key, LOG_LEVEL.Env)
}

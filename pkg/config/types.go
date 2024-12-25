package config

// XXX: Config file should have a version field to manage future changes to schema

type (
	// Cedana configuration. Each of the below fields can also be set
	// through an environment variable with the same name, prefixed, and in uppercase. E.g.
	// `Metrics.ASR` can be set with `CEDANA_METRICS_ASR`. The `env_aliases` tag below specifies
	// alternative (alias) environment variable names (comma-separated).
	Config struct {
		// Port is the port on which the daemon listens for incoming connections
		Port uint32 `json:"port"      mapstructure:"port"`
		// Host is the address on which the daemon listens for incoming connections
		Host string `json:"host"      mapstructure:"host"`
		// ContextID is the context ID to use for VSOCK connections
		ContextID uint32 `json:"context_id" mapstructure:"context_id"`
		// UseVSOCK sets whether to use VSOCK instead for connections
		UseVSOCK bool `json:"use_vsock" mapstructure:"use_vsock"`
		// LogLevel is the log level to use for the daemon
		LogLevel string `json:"log_level" mapstructure:"log_level"`

		// Connection settings
		Connection Connection `json:"connection" mapstructure:"connection"`
		// Checkpoint and storage settings
		Checkpoints Checkpoints `json:"checkpoints" mapstructure:"checkpoints"`
		// Database details
		DB DB `json:"db"         mapstructure:"db"`
		// Profiling settings
		Profiling Profiling `json:"profiling"  mapstructure:"profiling"`
		// Metrics settings
		Metrics Metrics `json:"metrics"    mapstructure:"metrics"`
		// CRIU settings and defaults
		CRIU CRIU `json:"criu"       mapstructure:"criu"`
		// CLI settings
		CLI CLI `json:"cli"        mapstructure:"cli"`
		// GPU settings
		GPU GPU `json:"gpu"        mapstructure:"gpu"`
	}

	Connection struct {
		// URL is your unique Cedana endpoint URL
		URL string `json:"url"    mapstructure:"url" env_aliases:"CEDANA_URL"`
		// AuthToken is your authentication token for the Cedana endpoint
		AuthToken string `json:"auth_token" mapstructure:"auth_token" env_aliases:"CEDANA_AUTH_TOKEN"`
	}

	Checkpoints struct {
		// Dir is the default directory to store checkpoints
		Dir string `json:"dir"         mapstructure:"dir"`
		// Compression is the default compression algorithm to use for checkpoints
		Compression string `json:"compression" mapstructure:"compression"`
	}

	DB struct {
		// Remote sets whether to use a remote database
		Remote bool `json:"remote"      mapstructure:"remote" env_aliases:"CEDANA_REMOTE"`
		// Path is the local path to the database file. E.g. /tmp/cedana.db
		Path string `json:"path" mapstructure:"path"`
		// Whether to leave dead jobs in DB, even if there were no checkpoints made
		LeaveDead bool `json:"leave_dead" mapstructure:"leave_dead"`
	}

	Profiling struct {
		// Enabled sets whether to enable and show profiling information
		Enabled bool `json:"enabled" mapstructure:"enabled"`
	}

	Metrics struct {
		// ASR sets whether to enable ASR metrics
		ASR bool `json:"asr"  mapstructure:"asr"`
		// Otel sets whether to enable OpenTelemetry metrics
		Otel bool `json:"otel" mapstructure:"otel" env_aliases:"CEDANA_OTEL_ENABLED"`
	}

	CLI struct {
		// Wait for ready sets CLI commands to block if the daemon is not up yet
		WaitForReady bool `json:"wait_for_ready" mapstructure:"wait_for_ready"`
	}

	CRIU struct {
		// BinaryPath is a custom path to the CRIU binary
		BinaryPath string `json:"binary_path"   mapstructure:"binary_path"`
		// LeaveRunning sets whether to leave the process running after checkpoint
		LeaveRunning bool `json:"leave_running" mapstructure:"leave_running"`
	}

	GPU struct {
		// Number of warm GPU controllers to keep in pool
		PoolSize int `json:"pool_size" mapstructure:"pool_size"`
	}
)

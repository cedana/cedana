package config

// XXX: Config file should have a version field to manage future changes to schema

type (
	// Cedana configuration. Each of the below fields can also be set
	// through an environment variable with the same name, prefixed, and in uppercase. E.g.
	// `Metrics.ASR` can be set with `CEDANA_METRICS_ASR`. The `env_aliases` tag below specifies
	// alternative (alias) environment variable names (comma-separated).
	Config struct {
		// Address to use for incoming/outgoing connections
		Address string `json:"address" mapstructure:"address" yaml:"address"`
		// Protocol to use for incoming/outgoing connections (TCP, UNIX, VSOCK)
		Protocol string `json:"protocol" mapstructure:"protocol" yaml:"protocol"`
		// LogLevel is the default log level used by the server
		LogLevel string `json:"log_level" mapstructure:"log_level" yaml:"log_level"`

		// Connection settings
		Connection Connection `json:"connection" mapstructure:"connection" yaml:"connection"`
		// Checkpoint and storage settings
		Checkpoint Checkpoint `json:"checkpoint" mapstructure:"checkpoint" yaml:"checkpoint"`
		// Database details
		DB DB `json:"db"         mapstructure:"db" yaml:"db"`
		// Profiling settings
		Profiling Profiling `json:"profiling" mapstructure:"profiling" yaml:"profiling"`
		// Metrics settings
		Metrics Metrics `json:"metrics" mapstructure:"metrics" yaml:"metrics"`
		// CRIU settings and defaults
		CRIU CRIU `json:"criu" mapstructure:"criu" yaml:"criu"`
		// CLI settings
		CLI CLI `json:"cli" mapstructure:"cli" yaml:"cli"`
		// GPU settings
		GPU GPU `json:"gpu"        mapstructure:"gpu" yaml:"gpu"`
	}

	Connection struct {
		// URL is your unique Cedana endpoint URL
		URL string `json:"url"    mapstructure:"url" yaml:"url" env_aliases:"CEDANA_URL"`
		// AuthToken is your authentication token for the Cedana endpoint
		AuthToken string `json:"auth_token" mapstructure:"auth_token" yaml:"auth_token" env_aliases:"CEDANA_AUTH_TOKEN"`
	}

	Checkpoint struct {
		// Dir is the default directory to store checkpoints
		Dir string `json:"dir"         mapstructure:"dir" yaml:"dir"`
		// Compression is the default compression algorithm to use for checkpoints
		Compression string `json:"compression" mapstructure:"compression" yaml:"compression"`
	}

	DB struct {
		// Remote sets whether to use a remote database
		Remote bool `json:"remote"      mapstructure:"remote"  yaml:"remote" env_aliases:"CEDANA_REMOTE"`
		// Path is the local path to the database file. E.g. /tmp/cedana.db
		Path string `json:"path" mapstructure:"path" yaml:"path"`
	}

	Profiling struct {
		// Enabled sets whether to enable and show profiling information
		Enabled bool `json:"enabled" mapstructure:"enabled" yaml:"enabled"`
		// Precision sets the time precision when printing profiling information (auto, ns, us, ms, s)
		Precision string `json:"precision" mapstructure:"precision" yaml:"precision"`
	}

	Metrics struct {
		// ASR sets whether to enable ASR metrics
		ASR bool `json:"asr"  mapstructure:"asr" yaml:"asr"`
		// Otel sets whether to enable OpenTelemetry metrics
		Otel bool `json:"otel" mapstructure:"otel" yaml:"otel" env_aliases:"CEDANA_OTEL_ENABLED"`
	}

	CLI struct {
		// Wait for ready sets CLI commands to block if the daemon is not up yet
		WaitForReady bool `json:"wait_for_ready" mapstructure:"wait_for_ready" yaml:"wait_for_ready"`
	}

	CRIU struct {
		// BinaryPath is a custom path to the CRIU binary
		BinaryPath string `json:"binary_path"   mapstructure:"binary_path" yaml:"binary_path"`
		// LeaveRunning sets whether to leave the process running after checkpoint
		LeaveRunning bool `json:"leave_running" mapstructure:"leave_running" yaml:"leave_running"`
	}

	GPU struct {
		// Number of warm GPU controllers to keep in pool
		PoolSize int `json:"pool_size" mapstructure:"pool_size" yaml:"pool_size"`
	}
)

package config

// XXX: Config file should have a version field to manage future changes to schema

type (
	// Cedana configuration. Each of the below fields can also be set
	// through an environment variable with the same name, prefixed, and in uppercase. E.g.
	// `Metrics.ASR` can be set with `CEDANA_METRICS_ASR`. The `env_aliases` tag below specifies
	// alternative (alias) environment variable names (comma-separated).
	Config struct {
		// Address to use for incoming/outgoing connections
		Address string `json:"address" key:"address" yaml:"address" mapstructure:"address"`
		// Protocol to use for incoming/outgoing connections (TCP, UNIX, VSOCK)
		Protocol string `json:"protocol" key:"protocol" yaml:"protocol" mapstructure:"protocol"`
		// LogLevel is the default log level used by the server
		LogLevel string `json:"log_level" key:"log_level" yaml:"log_level" mapstructure:"log_level"`
		// LogLevelNoServer is the log level used when direct --no-server run/restore is used. This is separate from LogLevel so as to avoid cluttering the process output.
		LogLevelNoServer string `json:"log_level_no_server" key:"log_level_no_server" yaml:"log_level_no_server" mapstructure:"log_level_no_server"`

		// Connection settings
		Connection Connection `json:"connection" key:"connection" yaml:"connection" mapstructure:"connection"`
		// Checkpoint and storage settings
		Checkpoint Checkpoint `json:"checkpoint" key:"checkpoint" yaml:"checkpoint" mapstructure:"checkpoint"`
		// Database details
		DB DB `json:"db" key:"db" yaml:"db" mapstructure:"db"`
		// Profiling settings
		Profiling Profiling `json:"profiling" key:"profiling" yaml:"profiling" mapstructure:"profiling"`
		// Metrics settings
		Metrics Metrics `json:"metrics" key:"metrics" yaml:"metrics" mapstructure:"metrics"`
		// Client settings
		Client Client `json:"client" key:"client" yaml:"client" mapstructure:"client"`
		// CRIU settings and defaults
		CRIU CRIU `json:"criu" key:"criu" yaml:"criu" mapstructure:"criu"`
		// GPU is settings for the GPU plugin
		GPU GPU `json:"gpu" key:"gpu" yaml:"gpu" mapstructure:"gpu"`
		// Plugin settings
		Plugins Plugins `json:"plugins" key:"plugins" yaml:"plugins" mapstructure:"plugins"`
	}

	Connection struct {
		// URL is your unique Cedana endpoint URL
		URL string `json:"url" key:"url" yaml:"url" mapstructure:"url" env_aliases:"CEDANA_URL"`
		// AuthToken is your authentication token for the Cedana endpoint
		AuthToken string `json:"auth_token" key:"auth_token" yaml:"auth_token" mapstructure:"auth_token" env_aliases:"CEDANA_AUTH_TOKEN"`
	}

	Checkpoint struct {
		// Dir is the default directory to store checkpoints
		Dir string `json:"dir" key:"dir" yaml:"dir" mapstructure:"dir"`
		// Compression is the default compression algorithm to use for checkpoints
		Compression string `json:"compression" key:"compression" yaml:"compression" mapstructure:"compression"`
		// Stream (for streaming checkpoints) specifies the number of parallel streams to use.
		// 0 means no streaming. n > 0 means n parallel streams (or number of pipes) to use.
		Stream int32 `json:"stream" key:"stream" yaml:"stream" mapstructure:"stream"`
	}

	DB struct {
		// Remote sets whether to use a remote database
		Remote bool `json:"remote" key:"remote"  yaml:"remote" mapstructure:"remote" env_aliases:"CEDANA_REMOTE"`
		// Path is the local path to the database file. E.g. /tmp/cedana.db
		Path string `json:"path" key:"path" yaml:"path" mapstructure:"path"`
	}

	Profiling struct {
		// Enabled sets whether to enable and show profiling information
		Enabled bool `json:"enabled" key:"enabled" yaml:"enabled" mapstructure:"enabled"`
		// Precision sets the time precision when printing profiling information (auto, ns, us, ms, s)
		Precision string `json:"precision" key:"precision" yaml:"precision" mapstructure:"precision"`
	}

	Metrics struct {
		// ASR sets whether to enable ASR metrics
		ASR bool `json:"asr" key:"asr" yaml:"asr" mapstructure:"asr"`
		// Otel sets whether to enable OpenTelemetry metrics
		Otel bool `json:"otel" key:"otel" yaml:"otel" mapstructure:"otel" env_aliases:"CEDANA_OTEL_ENABLED"`
	}

	Client struct {
		// Wait for ready ensures client requests block if the daemon is not up yet
		WaitForReady bool `json:"wait_for_ready" key:"wait_for_ready" yaml:"wait_for_ready" mapstructure:"wait_for_ready" env_aliases:"CEDANA_WAIT_FOR_READY"`
	}

	CRIU struct {
		// BinaryPath is a custom path to the CRIU binary
		BinaryPath string `json:"binary_path" key:"binary_path" yaml:"binary_path" mapstructure:"binary_path"`
		// LeaveRunning sets whether to leave the process running after checkpoint
		LeaveRunning bool `json:"leave_running" key:"leave_running" yaml:"leave_running" mapstructure:"leave_running"`
	}

	GPU struct {
		// Number of warm GPU controllers to keep in pool
		PoolSize int `json:"pool_size" key:"pool_size" yaml:"pool_size" mapstructure:"pool_size"`
		// LogDir is the directory to write GPU logs to
		LogDir string `json:"log_dir" key:"log_dir" yaml:"log_dir" mapstructure:"log_dir"`
		// Track metrics associated with observability
		Observability bool `json:"observability" key:"observability" yaml:"observability" mapstructure:"observability"`
		// MultiprocessType is the type of multiprocess support to use (IPC, NCCL)
		MultiprocessType string `json:"multiprocess_type" key:"multiprocess_type" yaml:"multiprocess_type" mapstructure:"multiprocess_type"`
		// ShmSize is the size in bytes of the shared memory segment to use for GPU processes
		ShmSize int `json:"shm_size" key:"shm_size" yaml:"shm_size" mapstructure:"shm_size"`
	}

	Plugins struct {
		// BinDir is the directory where plugin binaries are stored
		BinDir string `json:"bin_dir" key:"bin_dir" yaml:"bin_dir" mapstructure:"bin_dir"`
		// LibDir is the directory where plugin libraries are stored
		LibDir string `json:"lib_dir" key:"lib_dir" yaml:"lib_dir" mapstructure:"lib_dir" env_aliases:"CEDANA_PLUGINS_LIB_DIR"`
		// Builds is the build versions to list/download for plugins (release, alpha)
		Builds string `json:"builds" key:"builds" yaml:"builds" mapstructure:"builds"`
	}
)

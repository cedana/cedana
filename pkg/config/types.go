package config

// XXX: Config file should have a version field to manage future changes to schema

type (
	// Cedana configuration. Each of the below fields can also be set
	// through an environment variable with the same name, prefixed, and in uppercase. E.g.
	// `Checkpoint.Dir` can be set with `CEDANA_CHECKPOINT_DIR`. The `env_aliases` tag below specifies
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
		// Metrics is whether to enable metrics collection and observability
		Metrics bool `json:"metrics" key:"metrics" yaml:"metrics" mapstructure:"metrics"`

		// Connection settings
		Connection Connection `json:"connection" key:"connection" yaml:"connection" mapstructure:"connection"`
		// Checkpoint and storage settings
		Checkpoint Checkpoint `json:"checkpoint" key:"checkpoint" yaml:"checkpoint" mapstructure:"checkpoint"`
		// Database details
		DB DB `json:"db" key:"db" yaml:"db" mapstructure:"db"`
		// Profiling settings
		Profiling Profiling `json:"profiling" key:"profiling" yaml:"profiling" mapstructure:"profiling"`
		// Client settings
		Client Client `json:"client" key:"client" yaml:"client" mapstructure:"client"`
		// CRIU settings and defaults
		CRIU CRIU `json:"criu" key:"criu" yaml:"criu" mapstructure:"criu"`
		// GPU is settings for the GPU plugin
		GPU GPU `json:"gpu" key:"gpu" yaml:"gpu" mapstructure:"gpu"`
		// Plugin settings
		Plugins Plugins `json:"plugins" key:"plugins" yaml:"plugins" mapstructure:"plugins"`

		// AWS settings
		AWS AWS `json:"aws" key:"aws" yaml:"aws" mapstructure:"aws"`
	}

	Connection struct {
		// URL is your unique Cedana endpoint URL
		URL string `json:"url" key:"url" yaml:"url" mapstructure:"url" env_aliases:"CEDANA_URL"`
		// AuthToken is your authentication token for the Cedana endpoint
		AuthToken string `json:"auth_token" key:"auth_token" yaml:"auth_token" mapstructure:"auth_token" env_aliases:"CEDANA_AUTH_TOKEN"`
	}

	Checkpoint struct {
		// Dir is the default directory to store checkpoints
		// - "cedana://<path>" for Cedana-managed global storage (recommended)
		// - "s3://<path>" for your S3 storage
		// - "<path>" for node-local storage
		Dir string `json:"dir" key:"dir" yaml:"dir" mapstructure:"dir"`
		// Compression is the default compression algorithm to use for checkpoints
		Compression string `json:"compression" key:"compression" yaml:"compression" mapstructure:"compression"`
		// Streams specifies the number of parallel streams to use when checkpointing.
		Streams int32 `json:"streams" key:"streams" yaml:"streams" mapstructure:"streams"`
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

	Client struct {
		// Wait for ready ensures client requests block if the daemon is not up yet
		WaitForReady bool `json:"wait_for_ready" key:"wait_for_ready" yaml:"wait_for_ready" mapstructure:"wait_for_ready" env_aliases:"CEDANA_WAIT_FOR_READY"`
	}

	CRIU struct {
		// BinaryPath is a custom path to the CRIU binary
		BinaryPath string `json:"binary_path" key:"binary_path" yaml:"binary_path" mapstructure:"binary_path"`
		// LeaveRunning sets whether to leave the process running after checkpoint
		LeaveRunning bool `json:"leave_running" key:"leave_running" yaml:"leave_running" mapstructure:"leave_running"`
		// ManageCgroups sets the default cgroup C/R mode for CRIU (none, props, soft, full, strict, ignore)
		ManageCgroups string `json:"manage_cgroups" key:"manage_cgroups" yaml:"manage_cgroups" mapstructure:"manage_cgroups"`
	}

	GPU struct {
		// Number of warm GPU controllers to keep in pool
		PoolSize int `json:"pool_size" key:"pool_size" yaml:"pool_size" mapstructure:"pool_size"`
		// LogDir is the directory to write GPU logs to
		LogDir string `json:"log_dir" key:"log_dir" yaml:"log_dir" mapstructure:"log_dir"`
		// SockDir is the directory to use for the GPU sockets
		SockDir string `json:"sock_dir" key:"sock_dir" yaml:"sock_dir" mapstructure:"sock_dir"`
		// Track metrics associated with observability
		Observability bool `json:"observability" key:"observability" yaml:"observability" mapstructure:"observability"`
		// FreezeType is the type of freeze to use for GPU processes (IPC, NCCL)
		FreezeType string `json:"freeze_type" key:"freeze_type" yaml:"freeze_type" mapstructure:"freeze_type"`
		// ShmSize is the size in bytes of the shared memory segment to use for GPU processes
		ShmSize uint64 `json:"shm_size" key:"shm_size" yaml:"shm_size" mapstructure:"shm_size"`
		// LdLibPath holds any additional directories to search for GPU libraries
		LdLibPath string `json:"ld_lib_path" key:"ld_lib_path" yaml:"ld_lib_path" mapstructure:"ld_lib_path"`
		// Debug enables debugging capabilities for the GPU plugin. Daemon will try to attach to existing running GPU controllers
		Debug bool `json:"debug" key:"debug" yaml:"debug" mapstructure:"debug"`
	}

	Plugins struct {
		// BinDir is the directory where plugin binaries are stored
		BinDir string `json:"bin_dir" key:"bin_dir" yaml:"bin_dir" mapstructure:"bin_dir"`
		// LibDir is the directory where plugin libraries are stored
		LibDir string `json:"lib_dir" key:"lib_dir" yaml:"lib_dir" mapstructure:"lib_dir" env_aliases:"CEDANA_PLUGINS_LIB_DIR"`
		// Builds is the build versions to list/download for plugins (release, alpha)
		Builds string `json:"builds" key:"builds" yaml:"builds" mapstructure:"builds"`
	}

	AWS struct {
		// AccessKeyID is the AWS access key ID
		AccessKeyID string `json:"access_key_id" key:"access_key_id" yaml:"access_key_id" mapstructure:"access_key_id" env_aliases:"AWS_ACCESS_KEY_ID"`
		// SecretAccessKey is the AWS secret access key
		SecretAccessKey string `json:"secret_access_key" key:"secret_access_key" yaml:"secret_access_key" mapstructure:"secret_access_key" env_aliases:"AWS_SECRET_ACCESS_KEY"`
		// Region is the AWS region to use (uses default region if not set)
		Region string `json:"region" key:"region" yaml:"region" mapstructure:"region" env_aliases:"AWS_REGION"`
	}
)

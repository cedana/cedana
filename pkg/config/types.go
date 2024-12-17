package config

// XXX: Config file should have a version field to manage future changes to schema
type (
	Config struct {
		Port      uint32 `json:"port"      mapstructure:"port"`
		Host      string `json:"host"      mapstructure:"host"`
		ContextID uint32 `json:"context_id" mapstructure:"context_id"`
		UseVSOCK  bool   `json:"use_vsock" mapstructure:"use_vsock"`
		LogLevel  string `json:"log_level" mapstructure:"log_level"`

		Connection Connection `json:"connection" mapstructure:"connection"`
		Storage    Storage    `json:"storage"    mapstructure:"storage"`
		Profiling  Profiling  `json:"profiling"  mapstructure:"profiling"`
		Metrics    Metrics    `json:"metrics"    mapstructure:"metrics"`
		CRIU       CRIU       `json:"criu"       mapstructure:"criu"`
		CLI        CLI        `json:"cli"        mapstructure:"cli"`
		GPU        GPU        `json:"gpu"        mapstructure:"gpu"`
	}
	Connection struct {
		URL       string `json:"url"    mapstructure:"url"`
		AuthToken string `json:"auth_token" mapstructure:"auth_token"`
	}
	Storage struct {
		Remote      bool   `json:"remote"      mapstructure:"remote"`
		DumpDir     string `json:"dump_dir"    mapstructure:"dump_dir"`
		Compression string `json:"compression" mapstructure:"compression"`
	}
	Profiling struct {
		Enabled bool `json:"enabled" mapstructure:"enabled"`
	}
	Metrics struct {
		ASR  bool `json:"asr"  mapstructure:"asr"`
		Otel Otel `json:"otel" mapstructure:"otel"`
	}
	CLI struct {
		WaitForReady bool `json:"wait_for_ready" mapstructure:"wait_for_ready"`
	}
	CRIU struct {
		BinaryPath   string `json:"binary_path"   mapstructure:"binary_path"`
		LeaveRunning bool   `json:"leave_running" mapstructure:"leave_running"`
	}
	GPU struct {
		// Number of warm GPU controllers to keep in pool
		PoolSize int `json:"pool_size" mapstructure:"pool_size"`
	}
	Otel struct {
		Enabled bool `json:"enabled" mapstructure:"enabled"`
		Port    int  `json:"port" mapstructure:"port"`
	}
)

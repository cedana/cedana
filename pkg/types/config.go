package types

// XXX: Config file should have a version field to manage future changes to schema
type (
	// Daemon config
	Config struct {
		Options    Options    `key:"options" json:"options" mapstructure:"options"`
		Connection Connection `key:"connection" json:"connection" mapstructure:"connection"`
		Storage    Storage    `key:"storage" json:"storage" mapstructure:"storage"`
		Profiling  Profiling  `key:"profiling" json:"profiling" mapstructure:"profiling"`
		CLI        CLI        `key:"cli" json:"cli" mapstructure:"cli"`
	}
	Options struct {
		LeaveRunning bool `key:"leaveRunning" json:"leave_running" mapstructure:"leave_running"`
	}
	Connection struct {
		// for cedana managed systems
		CedanaUrl       string `key:"cedanaUrl" json:"cedana_url" mapstructure:"cedana_url"`
		CedanaAuthToken string `key:"cedanaAuthToken" json:"cedana_auth_token" mapstructure:"cedana_auth_token"`
	}
	Storage struct {
		Remote  bool   `key:"remote" json:"remote" mapstructure:"remote"`
		DumpDir string `key:"dumpDir" json:"dump_dir" mapstructure:"dump_dir"`
	}
	Profiling struct {
		Enabled bool `key:"enabled" json:"enabled" mapstructure:"enabled"`
		Otel    struct {
			Port int `key:"port" json:"port" mapstructure:"port"`
		} `key:"otel" json:"otel" mapstructure:"otel"`
	}
	CLI struct {
		WaitForReady bool `key:"waitForReady" json:"wait_for_ready" mapstructure:"wait_for_ready"`
	}
)

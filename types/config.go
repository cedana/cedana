package types

// XXX: Config file should have a version field to manage future changes to schema
type (
	// Daemon config
	Config struct {
		Client        Client        `json:"client" mapstructure:"client"`
		Connection    Connection    `json:"connection" mapstructure:"connection"`
		SharedStorage SharedStorage `json:"shared_storage" mapstructure:"shared_storage"`
	}
	Client struct {
		// job to run
		Task         string `json:"task" mapstructure:"task"`
		LeaveRunning bool   `json:"leave_running" mapstructure:"leave_running"`
		ForwardLogs  bool   `json:"forward_logs" mapstructure:"forward_logs"`
	}
	Connection struct {
		// for cedana managed systems
		CedanaUrl       string `json:"cedana_url" mapstructure:"cedana_url"`
		CedanaAuthToken string `json:"cedana_auth_token" mapstructure:"cedana_auth_token"`
	}
	SharedStorage struct {
		DumpStorageDir string `json:"dump_storage_dir" mapstructure:"dump_storage_dir"`
	}

	// CLI config
	ConfigCLI struct {
		WaitForReady bool `json:"wait_for_ready" mapstructure:"wait_for_ready"`
	}

	// Misc
	InitConfigArgs struct {
		Config    string
		ConfigDir string
	}
)

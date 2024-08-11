package types

// XXX: Config file should have a version field to manage future changes to schema
type (
	// Daemon config
	Config struct {
		Client        Client        `key:"client" json:"client" mapstructure:"client"`
		Connection    Connection    `key:"connection" json:"connection" mapstructure:"connection"`
		SharedStorage SharedStorage `key:"sharedStorage" json:"shared_storage" mapstructure:"shared_storage"`
	}
	Client struct {
		// job to run
		Task         string `key:"task" json:"task" mapstructure:"task"`
		LeaveRunning bool   `key:"leaveRunning" json:"leave_running" mapstructure:"leave_running"`
		ForwardLogs  bool   `key:"forwardLogs" json:"forward_logs" mapstructure:"forward_logs"`
	}
	Connection struct {
		// for cedana managed systems
		CedanaUrl       string `key:"cedanaUrl" json:"cedana_url" mapstructure:"cedana_url"`
		CedanaAuthToken string `key:"cedanaAuthToken" json:"cedana_auth_token" mapstructure:"cedana_auth_token"`
	}
	SharedStorage struct {
		DumpStorageDir string `key:"dumpStorageDir" json:"dump_storage_dir" mapstructure:"dump_storage_dir"`
	}

	// CLI config
	ConfigCLI struct {
		WaitForReady bool `key:"waitForReady" json:"wait_for_ready" mapstructure:"wait_for_ready"`
	}

	// Misc
	InitConfigArgs struct {
		Config    string
		ConfigDir string
	}
)

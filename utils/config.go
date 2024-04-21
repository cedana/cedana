package utils

import (
	"os"
	"os/user"
	"path/filepath"

	"github.com/spf13/viper"
)

const (
	configDirName  = ".cedana"
	configFileName = "client_config"
	configFileType = "json"
	envVarPrefix   = "CEDANA"
	configDirPerm  = 0755
	configFilePerm = 0644
)

// XXX: Config file should have a version field to manage future changes to schema
type (
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
)

func InitConfig() error {
	user, err := getUser()
	if err != nil {
		return err
	}

	homeDir := user.HomeDir
	configDir := filepath.Join(homeDir, configDirName)

	viper.AddConfigPath(configDir)
	viper.SetConfigPermissions(configFilePerm)
	viper.SetConfigType(configFileType)
	viper.SetConfigName(configFileName)
	viper.SetEnvPrefix(envVarPrefix)

	// Allow environment variables to be accesses through viper *if* bound.
	// For e.g. CEDANA_SECRET will be accessible as viper.Get("secret")
	// However, viper.Get() always first checks the config file
	viper.AutomaticEnv()

	// Create config directory if it does not exist
	_, err = os.Stat(configDir)
	if os.IsNotExist(err) {
		err = os.MkdirAll(configDir, configDirPerm)
		if err != nil {
			return err
		}
	}

	setDefaults()
	bindEnvVars()

	err = viper.SafeWriteConfig() // Will only overwrite if file does not exist
	if err != nil {
		return err
	}

	return viper.ReadInConfig()
}

// Set defaults that are used when no value is found in config/env vars
func setDefaults() {
	viper.SetDefault("client.process_name", "")
	viper.SetDefault("client.leave_running", "")

	viper.SetDefault("shared_storage.dump_storage_dir", "/tmp")

	viper.SetDefault("connection.cedana_url", "0.0.0.0")
	viper.SetDefault("connection.cedana_user", "random-user")
	viper.SetDefault("connection.cedana_auth_token", "random-token")
}

// TODO: Add bindings for env vars so env vars can be used as backup
// when a value is not found in config
func bindEnvVars() {
}

func getUser() (*user.User, error) {
	username := os.Getenv("SUDO_USER")
	if username == "" {
		username = os.Getenv("USER")
	}
	return user.Lookup(username)
}

package utils

import (
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"

	"github.com/spf13/viper"
)

type Config struct {
	CedanaManaged bool          `json:"cedana_managed" mapstructure:"cedana_managed"`
	Client        Client        `json:"client" mapstructure:"client"`
	ActionScripts ActionScripts `json:"action_scripts" mapstructure:"action_scripts"`
	Connection    Connection    `json:"connection" mapstructure:"connection"`
	SharedStorage SharedStorage `json:"shared_storage" mapstructure:"shared_storage"`
}

type Client struct {
	// job to run
	Task                 string `json:"task" mapstructure:"task"`
	LeaveRunning         bool   `json:"leave_running" mapstructure:"leave_running"`
	ForwardLogs          bool   `json:"forward_logs" mapstructure:"forward_logs"`
	SignalProcessPreDump bool   `json:"signal_process_pre_dump" mapstructure:"signal_process_pre_dump"`
	SignalProcessTimeout int    `json:"signal_process_timeout" mapstructure:"signal_process_timeout"`
}

type ActionScripts struct {
	PreDump    string `json:"pre_dump" mapstructure:"pre_dump"`
	PostDump   string `json:"post_dump" mapstructure:"post_dump"`
	PreRestore string `json:"pre_restore" mapstructure:"pre_restore"`
}

type Connection struct {
	NATSUrl       string `json:"nats_url" mapstructure:"nats_url"`
	NATSPort      int    `json:"nats_port" mapstructure:"nats_port"`
	NATSAuthToken string `json:"nats_auth_token" mapstructure:"nats_auth_token"`
	// for cedana managed systems
	CedanaUrl       string `json:"cedana_url" mapstructure:"cedana_url"`
	CedanaAuthToken string `json:"cedana_auth_token" mapstructure:"cedana_auth_token"`
}

type SharedStorage struct {
	DumpStorageDir string `json:"dump_storage_dir" mapstructure:"dump_storage_dir"`
}

func InitConfig() (*Config, error) {
	var username string
	// have to run cedana as root, but it overrides os.UserHomeDir w/ /root
	username = os.Getenv("SUDO_USER")
	if username == "" {
		username = os.Getenv("USER")
	}

	u, err := user.Lookup(username)
	if err != nil {
		return nil, err
	}

	homedir := u.HomeDir
	viper.AddConfigPath(filepath.Join(homedir, ".cedana/"))
	viper.SetConfigType("json")
	viper.SetConfigName("client_config")

	// InitConfig should do the testing for path
	_, err = os.OpenFile(filepath.Join(homedir, ".cedana", "client_config.json"), 0, 0o644)
	if errors.Is(err, os.ErrNotExist) {
		fmt.Println("client_config.json does not exist, creating sample config...")
		_, err = os.Create(filepath.Join(homedir, ".cedana", "client_config.json"))
		if err != nil {
			panic(fmt.Errorf("error creating config file: %v", err))
		}
		// Set some dummy defaults, that are only loaded if the file doesn't exist
		viper.Set("client.process_name", "someProcess")
		viper.Set("client.leave_running", true)
		viper.WriteConfig()
	}

	viper.AutomaticEnv()

	var config Config
	err = viper.ReadInConfig()
	if err != nil {
		panic(fmt.Errorf("error: %v, have you run bootstrap?, ", err))
	}

	if err := viper.Unmarshal(&config); err != nil {
		fmt.Println(err)
		return nil, err
	}
	so, err := LoadOverrides(filepath.Join(homedir, ".cedana"))

	// override setting is ugly, need to abstract this away somehow
	if err == nil && so != nil {
		viper.Set("cedana_managed", so.CedanaManaged)
		viper.Set("shared_storage.dump_storage_dir", so.SharedStorage.DumpStorageDir)
		viper.Set("connection.nats_url", so.Connection.NATSUrl)
		viper.Set("connection.nats_port", so.Connection.NATSPort)
		viper.Set("connection.auth_token", so.Connection.NATSAuthToken)
		viper.Set("client.task", so.Client.Task)
	}

	viper.WriteConfig()
	return &config, nil
}

// // write to disk

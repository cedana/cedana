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
	Docker        Docker        `json:"docker" mapstructure:"docker"`
	SharedStorage SharedStorage `json:"shared_storage" mapstructure:"shared_storage"`
}

type Client struct {
	ProcessName          string `json:"process_name" mapstructure:"process_name"`
	LeaveRunning         bool   `json:"leave_running" mapstructure:"leave_running"`
	SignalProcessPreDump bool   `json:"signal_process_pre_dump" mapstructure:"signal_process_pre_dump"`
	SignalProcessTimeout int    `json:"signal_process_timeout" mapstructure:"signal_process_timeout"`
}

type ActionScripts struct {
	PreDump    string `json:"pre_dump" mapstructure:"pre_dump"`
	PostDump   string `json:"post_dump" mapstructure:"post_dump"`
	PreRestore string `json:"pre_restore" mapstructure:"pre_restore"`
}

type Connection struct {
	NATSUrl  string `json:"nats_url" mapstructure:"nats_url"`
	NATSPort int    `json:"nats_port" mapstructure:"nats_port"`
}

type Docker struct {
	LeaveRunning  bool   `json:"leave_running" mapstructure:"leave_running"`
	ContainerName string `json:"container_name" mapstructure:"container_name"`
	CheckpointID  string `json:"checkpoint_id" mapstructure:"checkpoint_id"`
}

type SharedStorage struct {
	// only useful for multi-machine checkpoint/restore
	MountPoint     string `json:"mount_point" mapstructure:"mount_point"`
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
		// Set some dummy defaults, that are only loaded if the file doesn't exist.
		// If it does exist, this isn't called, so the dummy isn't an override.
		viper.Set("client.process_name", "cedana")
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
	// SharedStorage
	if err == nil && so != nil {
		viper.Set("cedana_managed", so.CedanaManaged)
		viper.Set("shared_storage.mount_point", so.SharedStorage.MountPoint)
		viper.Set("shared_storage.dump_storage_dir", so.SharedStorage.DumpStorageDir)
	}

	viper.WriteConfig()
	return &config, nil
}

// // write to disk

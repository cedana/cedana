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
	Client        Client        `json:"client" mapstructure:"client"`
	ActionScripts ActionScripts `json:"action_scripts" mapstructure:"action_scripts"`
	Connection    Connection    `json:"connection" mapstructure:"connection"`
	SharedStorage SharedStorage `json:"shared_storage" mapstructure:"shared_storage"`
}

type Client struct {
	// job to run
	Task         string `json:"task" mapstructure:"task"`
	LeaveRunning bool   `json:"leave_running" mapstructure:"leave_running"`
	ForwardLogs  bool   `json:"forward_logs" mapstructure:"forward_logs"`
}

type ActionScripts struct {
	PreDump    string `json:"pre_dump" mapstructure:"pre_dump"`
	PostDump   string `json:"post_dump" mapstructure:"post_dump"`
	PreRestore string `json:"pre_restore" mapstructure:"pre_restore"`
}

type Connection struct {
	// for cedana managed systems
	CedanaUrl       string `json:"cedana_url" mapstructure:"cedana_url"`
	CedanaUser      string `json:"cedana_user" mapstructure:"cedana_user"`
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
	_, err = os.OpenFile(filepath.Join(homedir, ".cedana", "client_config.json"), 0, 0o664)
	if errors.Is(err, os.ErrNotExist) {
		fmt.Println("client_config.json does not exist, creating sample config...")
		err = os.WriteFile(filepath.Join(homedir, ".cedana", "client_config.json"), []byte(GenSampleConfig()), 0o664)
		if err != nil {
			panic(fmt.Errorf("error writing sample to config file: %v", err))
		}
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
		viper.Set("shared_storage.dump_storage_dir", so.SharedStorage.DumpStorageDir)
		viper.Set("client.task", so.Client.Task)
	}

	viper.WriteConfig()
	return &config, nil
}

func GenSampleConfig() string {
	return `{
	"client": {
		"process_name": "",
		"leave_running": true
	},
	"shared_storage": {
		"dump_storage_dir": "/tmp"
	},
	"connection": {
		"cedana_url": "0.0.0.0",
		"cedana_user": "random-user",
		"cedana_auth_token": "random-token"
	}
	`
}

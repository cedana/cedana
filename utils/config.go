package utils

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"

	"github.com/spf13/viper"
)

type Config struct {
	Client        Client        `json:"client" mapstructure:"client"`
	Connection    Connection    `json:"connection" mapstructure:"connection"`
	SharedStorage SharedStorage `json:"shared_storage" mapstructure:"shared_storage"`
}

type Client struct {
	// job to run
	Task         string `json:"task" mapstructure:"task"`
	LeaveRunning bool   `json:"leave_running" mapstructure:"leave_running"`
	ForwardLogs  bool   `json:"forward_logs" mapstructure:"forward_logs"`
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

	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)

	homedir := u.HomeDir
	viper.AddConfigPath(filepath.Join(homedir, ".cedana/"))
	viper.SetConfigType("json")
	viper.SetConfigName("client_config")

	// check if folder exists, if it doesn't assume config doesn't exist either
	// Check if the .cedana folder exists, if not, create it
	_, err = os.Stat(filepath.Join(homedir, ".cedana"))
	if os.IsNotExist(err) {
		fmt.Println(".cedana folder doesn't exist, creating and populating with sample config...")
		err = os.MkdirAll(filepath.Join(homedir, ".cedana"), 0o777)
		if err != nil {
			panic(fmt.Errorf("error creating .cedana folder: %v", err))
		}
		// folder belongs to user
		os.Chown(filepath.Join(homedir, ".cedana"), uid, gid)
		fmt.Println("creating sample config...")
		err = os.WriteFile(filepath.Join(homedir, ".cedana", "client_config.json"), []byte(GenSampleConfig()), 0o666)
		if err != nil {
			panic(fmt.Errorf("error writing sample to config file: %v", err))
		}
		os.Chown(filepath.Join(homedir, ".cedana", "client_config.json"), uid, gid)

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
}`
}

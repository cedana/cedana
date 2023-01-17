package utils

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"

	"github.com/spf13/viper"
)

type Config struct {
	Client        Client        `mapstructure:"client"`
	ActionScripts ActionScripts `mapstructure:"action_scripts"`
	Connection    Connection    `mapstructure:"connection"`
	Docker        Docker        `mapstructure:"docker"`
	SharedStorage SharedStorage `mapstructure:"shared_storage"`
}

type Client struct {
	ProcessName      string `mapstructure:"process_name"`
	DumpFrequencyMin int    `mapstructure:"dump_frequency_min"`
	LeaveRunning     bool   `mapstructure:"leave_running"`
}

type ActionScripts struct {
	PreDump    string `mapstructure:"pre_dump"`
	PostDump   string `mapstructure:"post_dump"`
	PreRestore string `mapstructure:"pre_restore"`
}

type Connection struct {
	ServerAddr string `mapstructure:"server_addr"`
	ServerPort int    `mapstructure:"server_port"`
}

type Docker struct {
	LeaveRunning  bool   `mapstructure:"leave_running"`
	ContainerName string `mapstructure:"container_name"`
	CheckpointID  string `mapstructure:"checkpoint_id"`
}

type SharedStorage struct {
	EFSId string `mapstructure:"efs_id"`
	// only useful for multi-machine checkpoint/restore
	MountPoint     string `mapstructure:"shared_mount_point"`
	DumpStorageDir string `mapstructure:"dump_storage_dir"`
}

func InitConfig() (*Config, error) {
	// have to run cedana as root, but it overrides os.UserHomeDir w/ /root
	username := os.Getenv("SUDO_USER")
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
	so := *loadOverrides()

	// there HAS to be a better way to do this
	viper.Set("aws.efs_id", so.SharedStorage.EFSId)
	viper.Set("aws.efs_mountpoint", so.SharedStorage.MountPoint)

	viper.WriteConfig()
	return &config, nil
}

func loadOverrides() *Config {
	var serverOverrides Config

	// load override from file. Fail silently if it doesn't exist, or GenSampleConfig instead
	// overrides are added during instance setup/creation/instantiation (?)
	f, err := os.ReadFile(filepath.Join(os.Getenv("HOME"), ".cedana", "server_overrides.json"))
	if err != nil {
		return &Config{}
	} else {
		fmt.Printf("found server specified overrides, overriding config...\n")
		err = json.Unmarshal(f, &serverOverrides)
		if err != nil {
			fmt.Printf("some err: %v", err)
			// we don't care - drop and leave
			return &Config{}
		}
		return &serverOverrides
	}
}

// write to disk

package utils

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

type Config struct {
	Client        Client        `mapstructure:"client"`
	ActionScripts ActionScripts `mapstructure:"action_scripts"`
	Connection    Connection    `mapstructure:"connection"`
	Docker        Docker        `mapstructure:"docker"`
	AWS           AWS           `mapstructure:"aws"`
}

type Client struct {
	ProcessName      string `mapstructure:"process_name"`
	DumpFrequencyMin int    `mapstructure:"dump_frequency_min"`
	DumpStorageDir   string `mapstructure:"dump_storage_dir"`
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

type AWS struct {
	EFSId         string `mapstructure:"efs_id"`
	EFSMountPoint string `mapstructure:"efs_mount_point"`
}

func InitConfig() (*Config, error) {

	viper.AddConfigPath(filepath.Join(os.Getenv("HOME"), ".cedana/"))
	homedir, _ := os.UserHomeDir()
	viper.AddConfigPath(filepath.Join(homedir, ".cedana/"))
	viper.SetConfigType("json")
	viper.SetConfigName("client_config")

	// InitConfig should do the testing for path
	_, err := os.OpenFile(filepath.Join(homedir, ".cedana", "client_config.json"), 0, 0o644)
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
	viper.Set("aws.efs_id", so.AWS.EFSId)
	viper.Set("aws.efs_mountpoint", so.AWS.EFSMountPoint)

	viper.WriteConfig()
	return &config, nil
}

func loadOverrides() *Config {
	var serverOverrides Config

	// load override from file. Fail silently if it doesn't exist, or GenSampleConfig instead
	// overrides are added during instance setup/creation/instantiation (?)
	f, err := os.ReadFile(filepath.Join(os.Getenv("HOME"), ".cedana", "server_overrides.json"))
	if err != nil {
		fmt.Printf("error looking for server overrides %v", err)
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

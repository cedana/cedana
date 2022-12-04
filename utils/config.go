package utils

import (
	"fmt"

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
	EFSMountPoint string `mapstructure:"efs_id"`
}

func InitConfig() (*Config, error) {

	viper.AddConfigPath(".")
	viper.SetConfigType("yaml")
	viper.SetConfigName("client_config")

	viper.AutomaticEnv()

	var config Config
	err := viper.ReadInConfig()
	if err != nil {
		panic(fmt.Errorf("fatal error config file: %w", err))
	}

	if err := viper.Unmarshal(&config); err != nil {
		fmt.Println(err)
		return nil, err
	}
	return &config, nil
}

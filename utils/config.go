package utils

import (
	"encoding/json"
	"fmt"
	"os"

	pb "github.com/nravic/cedana/rpc"
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
	loadServerOverrides(&config)
	return &config, nil
}

func loadServerOverrides(c *Config) {
	var serverOverrides pb.ConfigClient

	// load override from file. Fail silently if it doesn't exist
	// overrides are added during instance setup/creation/instantiation (?)
	f, err := os.ReadFile("server_overrides.json")
	if err != nil {
		return
	} else {
		err = json.Unmarshal(f, &serverOverrides)
		if err != nil {
			// we don't care - drop and leave
			return
		}

		// no better way right now than just loading each override into the config

		// AWS Overrides
		c.AWS.EFSId = serverOverrides.Aws.EfsId
		c.AWS.EFSMountPoint = serverOverrides.Aws.EfsMountPoint

		// Connection Overrides
		c.Connection.ServerAddr = serverOverrides.Connection.ServerAddr
		c.Connection.ServerPort = int(serverOverrides.Connection.ServerPort)
	}
}

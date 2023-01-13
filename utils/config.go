package utils

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

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

	viper.AddConfigPath(filepath.Join(os.Getenv("HOME"), ".cedana/"))
	viper.SetConfigType("json")
	viper.SetConfigName("client_config")

	viper.AutomaticEnv()

	var config Config
	err := viper.ReadInConfig()
	if err != nil {
		panic(fmt.Errorf("error: %v, have you run bootstrap?, ", err))
	}

	if err := viper.Unmarshal(&config); err != nil {
		fmt.Println(err)
		return nil, err
	}
	so := *loadOverrides()
	aws := AWS{}
	if so.Aws != nil {
		aws.EFSId = so.Aws.EfsId
		aws.EFSMountPoint = so.Aws.EfsMountPoint
	}

	// no better way right now than just loading each override into the config
	viper.Set("efs_id", aws.EFSId)
	viper.Set("efs_mountpoint", aws.EFSMountPoint)

	viper.WriteConfig()
	return &config, nil
}

func loadOverrides() *pb.ConfigClient {
	var serverOverrides pb.ConfigClient

	// load override from file. Fail silently if it doesn't exist, or GenSampleConfig instead
	// overrides are added during instance setup/creation/instantiation (?)
	f, err := os.ReadFile(filepath.Join(os.Getenv("HOME"), ".cedana", "server_overrides.json"))
	if err != nil {
		fmt.Printf("error looking for server overrides %v", err)
		return &pb.ConfigClient{}
	} else {
		fmt.Printf("found server specified overrides, overriding config...\n")
		err = json.Unmarshal(f, &serverOverrides)
		if err != nil {
			fmt.Printf("some err: %v", err)
			// we don't care - drop and leave
			return &pb.ConfigClient{}
		}
		return &serverOverrides
	}
}

func GenSampleConfig(path string) error {
	c := Config{
		Client: Client{
			LeaveRunning:   true,         // sanest/least impact default for leaveRunning
			DumpStorageDir: "~/.cedana/", // folder likely exists
		},
	}

	b, err := json.Marshal(c)
	if err != nil {
		return fmt.Errorf("err: %v, could not marshal spot config struct to file", err)
	}
	err = os.WriteFile(path, b, 0o644)
	if err != nil {
		return fmt.Errorf("err: %v, could not write file to path %s", err, path)
	}

	return err
}

// write to disk

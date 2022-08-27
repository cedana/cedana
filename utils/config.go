package utils

import (
	"fmt"

	"github.com/spf13/viper"
)

type Config struct {
	Client     Client     `mapstructure:"client"`
	Connection Connection `mapstructure:"connection"`
}

type Client struct {
	ProcessName      string `mapstructure:"process_name"`
	DumpFrequencyMin int    `mapstructure:"dump_frequency_min"`
	DumpStorageDir   string `mapstructure:"dump_storage_dir"`
}

type Connection struct {
	ServerAddr string `mapstructure:"server_addr"`
	ServerPort int    `mapstructure:"server_port"`
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

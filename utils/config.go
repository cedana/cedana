package utils

import (
	"fmt"

	"github.com/spf13/viper"
)

func InitConfig() {

	// search for config in home dir w/ ".cobra"
	viper.AddConfigPath(".")
	viper.SetConfigType("yaml")
	viper.SetConfigName("client_config")

	viper.AutomaticEnv()

	err := viper.ReadInConfig()
	if err != nil { // Handle errors reading the config file
		panic(fmt.Errorf("fatal error config file: %w", err))
	}

}

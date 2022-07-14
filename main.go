package main

import (
	"fmt"
	"os"

	"github.com/nravic/oort/cmd"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func initConfig() {
	home, err := os.UserHomeDir()
	cobra.CheckErr(err)

	// search for config in home dir w/ ".cobra"
	viper.AddConfigPath(home)
	viper.SetConfigType("yaml")
	viper.SetConfigName(".cobra")

	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err == nil {
		fmt.Println("Using config file:", viper.ConfigFileUsed())
	}
}

func main() {
	initConfig()
	cmd.Execute()
}

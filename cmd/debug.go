package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/cedana/cedana/utils"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var debugCmd = &cobra.Command{
	Use:    "debug",
	Short:  "Functions/tools for debugging instances or testing new components",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return fmt.Errorf("run debug with one of its subcommands")
	},
}

var cfgCmd = &cobra.Command{
	Use: "config",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := utils.InitConfig()
		if err != nil {
			return err
		}
		fmt.Printf("config file used: %s", viper.GetViper().ConfigFileUsed())
		// pretty print config for debugging to make sure it's been loaded correctly
		prettyCfg, err := json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(prettyCfg))
		return nil
	},
}

// experimental, testing out debugging the container checkpointing
var containerCmd = &cobra.Command{
	Use: "container",
	RunE: func(cmd *cobra.Command, args []string) error {
		// id := args[0]
		// dir := args[1]

		// // err := container.Dump(dir, id)
		// // if err != nil {
		// // 	return err
		// // }

		return nil
	},
}

func init() {
	rootCmd.AddCommand(debugCmd)
	debugCmd.AddCommand(cfgCmd)
	debugCmd.AddCommand(containerCmd)
}

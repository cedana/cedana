package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/cedana/cedana/utils"
	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "manage configuration",
}

var showCmd = &cobra.Command{
	Use:   "show",
	Short: "show currently set configuration",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := utils.InitConfig()
		if err != nil {
			return err
		}
		prettycfg, err := json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			return err
		}
		fmt.Printf("config: %v\n", string(prettycfg))
		return nil
	},
}

var generateCmd = &cobra.Command{
	Use:   "generate",
	Short: "generate configuration",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := utils.GenSampleConfig()

		fmt.Printf("config: %v\n", string(cfg))
		return nil
	},
}

func init() {
	rootCmd.AddCommand(configCmd)
	configCmd.AddCommand(showCmd)
	configCmd.AddCommand(generateCmd)
}

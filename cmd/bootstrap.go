package cmd

// This file contains all the bootstrap-related commands when starting `cedana bootstrap ...`

import (
	"github.com/cedana/cedana/utils"
	"github.com/spf13/cobra"
)

// bootstrap the cedana client and load config overrides if they exist
// XXX: This cmd should be deprecated if not doing anything useful apart from loading config
// as it can be loaded on each execution - a simply *if* check is already creating the
// default config if it doesn't exist
var bootstrapCmd = &cobra.Command{
	Use:   "bootstrap",
	Short: "Setup host for cedana usage",
	Long:  "",
	RunE: func(cmd *cobra.Command, args []string) error {
		err := utils.InitConfig()
		if err != nil {
			logger.Fatal().Err(err).Msg("could not initiate generated config")
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(bootstrapCmd)
}

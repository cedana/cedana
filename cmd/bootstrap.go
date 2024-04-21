package cmd

import (
	"github.com/cedana/cedana/utils"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
)

type Bootstrap struct {
	logger *zerolog.Logger
}

// bootstrap the cedana client and load config overrides if they exist
// XXX: This cmd should be deprecated if not doing anything useful apart from loading config
// as it can be loaded on each execution - a simply *if* check is already creating the
// default config if it doesn't exist
var bootstrapCmd = &cobra.Command{
	Use:   "bootstrap",
	Short: "Setup host for cedana usage",
	Long:  "",
	RunE: func(cmd *cobra.Command, args []string) error {
		logger := utils.GetLogger()
		b := &Bootstrap{
			logger: &logger,
		}
		b.bootstrap()

		return nil
	},
}

func (b *Bootstrap) bootstrap() {
	err := utils.InitConfig()
	if err != nil {
		b.logger.Fatal().Err(err).Msg("could not initiate generated config")
	}
}

func init() {
	rootCmd.AddCommand(bootstrapCmd)
}

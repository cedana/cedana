package cmd

import (
	"github.com/cedana/cedana/utils"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
)

type Bootstrap struct {
	l *zerolog.Logger
}

// bootstrap the cedana client and load config overrides if they exist
var bootstrapCmd = &cobra.Command{
	Use:   "bootstrap",
	Short: "Setup host for cedana usage",
	Long:  "",
	RunE: func(cmd *cobra.Command, args []string) error {
		logger := utils.GetLogger()
		b := &Bootstrap{
			l: &logger,
		}
		b.bootstrap()

		return nil
	},
}

func (b *Bootstrap) bootstrap() {
	_, err := utils.InitConfig()
	if err != nil {
		b.l.Fatal().Err(err).Msg("could not initiate generated config")
	}
}

func init() {
	rootCmd.AddCommand(bootstrapCmd)
}

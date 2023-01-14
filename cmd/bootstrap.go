package cmd

import (
	"os"
	"path/filepath"

	"github.com/nravic/cedana/utils"
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
	// check that ./cedana folder exists
	homeDir := os.Getenv("HOME")
	configFolderPath := filepath.Join(homeDir, ".cedana")
	// check that $HOME/.cedana folder exists - create if it doesn't
	_, err := os.Stat(configFolderPath)
	if err != nil {
		b.l.Info().Msg("config folder doesn't exist, creating...")
		err = os.Mkdir(configFolderPath, 0o755)
		if err != nil {
			b.l.Fatal().Err(err).Msg("could not create config folder")
		}
	}

	// check configFolderPath for cfg
	// let InitConfig populate with overrides (if any)
	_, err = utils.InitConfig()
	if err != nil {
		b.l.Fatal().Err(err).Msg("could not initiate generatedconfig")
	}
}

func init() {
	rootCmd.AddCommand(bootstrapCmd)
}

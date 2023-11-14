package cmd

import (
	"fmt"
	"os"
	"path/filepath"

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
		b.l.Fatal().Err(err).Msg("could not initiate generated config")
	}

	err = b.createSystemdService()
	if err != nil {
		b.l.Fatal().Err(err).Msg("could not create systemd service")
	}
}

func (b *Bootstrap) createSystemdService() error {
	unitFilePath := "/etc/systemd/system/cedana.service"

	// Open the unit file for writing
	unitFile, err := os.Create(unitFilePath)
	if err != nil {
		return err
	}
	defer unitFile.Close()

	// Write the unit file content
	unitFileContents := `[Unit]
Description=Cedana Worker Daemon
After=network.target

[Service]
Type=forking
ExecStart=/usr/bin/cedana daemon start 
Restart=on-failure

[Install]
WantedBy=multi-user.target
`
	_, err = unitFile.WriteString(unitFileContents)
	if err != nil {
		return err
	}

	fmt.Printf("Systemd service file created at %s\n", unitFilePath)
	return nil
}

func init() {
	rootCmd.AddCommand(bootstrapCmd)
}

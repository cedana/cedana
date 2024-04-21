package cmd

import (
	"context"

	"github.com/cedana/cedana/utils"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "cedana",
	Short: "simple criu dump/restore client",
	Long: `
________  _______   ________  ________  ________   ________     
|\   ____\|\  ___ \ |\   ___ \|\   __  \|\   ___  \|\   __  \    
\ \  \___|\ \   __/|\ \  \_|\ \ \  \|\  \ \  \\ \  \ \  \|\  \   
 \ \  \    \ \  \_|/_\ \  \ \\ \ \   __  \ \  \\ \  \ \   __  \  
  \ \  \____\ \  \_|\ \ \  \_\\ \ \  \ \  \ \  \\ \  \ \  \ \  \ 
   \ \_______\ \_______\ \_______\ \__\ \__\ \__\\ \__\ \__\ \__\
    \|_______|\|_______|\|_______|\|__|\|__|\|__| \|__|\|__|\|__|
                                                                 
    ` +
		"\n Instance Brokerage, Orchestration and Migration System." +
		"\n Property of Cedana, Corp.",
}

var logger *zerolog.Logger

func Execute(ctx context.Context) error {
	if err := utils.InitConfig(); err != nil {
		logger.Error().Err(err).Msg("failed to initialize config")
		return err
	}
	return rootCmd.ExecuteContext(ctx)
}

func init() {
	rootCmd.Version = GetVersion()
	logger = utils.GetLogger()
}

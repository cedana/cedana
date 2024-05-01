package cmd

// This file contains the root commands when starting `cedana ...`

import (
	"context"

	"github.com/cedana/cedana/db"
	"github.com/cedana/cedana/utils"
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

func Execute(ctx context.Context) error {
	logger := utils.GetLogger()
	db := db.NewLocalDB()

	ctx = context.WithValue(ctx, "logger", logger)
	ctx = context.WithValue(ctx, "db", db)

	if err := utils.InitConfig(); err != nil {
		logger.Error().Err(err).Msg("failed to initialize config")
		return err
	}

	rootCmd.Version = GetVersion()

	return rootCmd.ExecuteContext(ctx)
}

package cmd

// This file contains the root commands when starting `cedana ...`

import (
	"context"

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

func Execute(ctx context.Context, version string) error {
	logger := utils.GetLogger()

	ctx = context.WithValue(ctx, "logger", logger)

	rootCmd.Version = version

	// only show usage when true usage error
	rootCmd.SilenceUsage = true

	return rootCmd.ExecuteContext(ctx)
}

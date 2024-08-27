package cmd

// This file contains the root commands when starting `cedana ...`

import (
	"context"

	"github.com/cedana/cedana/pkg/types"
	"github.com/cedana/cedana/pkg/utils"
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
		"\n Property of Cedana, Corp.\n",
}

func Execute(ctx context.Context, version string) error {
	logger := utils.GetLogger()

	ctx = context.WithValue(ctx, "logger", logger)

	rootCmd.Version = version
	rootCmd.Long = rootCmd.Long + "\n " + version

	// only show usage when true usage error
	rootCmd.SilenceUsage = true

	rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		config, _ := cmd.Flags().GetString(configFlag)
		configDir, _ := cmd.Flags().GetString(configDirFlag)
		if err := utils.InitConfig(types.InitConfigArgs{
			Config:    config,
			ConfigDir: configDir,
		}); err != nil {
			logger.Error().Err(err).Msg("failed to initialize config")
			return err
		}
		return nil
	}

	rootCmd.PersistentFlags().String(configFlag, "", "one-time config JSON string (will merge with existing config)")
	rootCmd.PersistentFlags().String(configDirFlag, "", "custom config directory")

	return rootCmd.ExecuteContext(ctx)
}

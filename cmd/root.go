package cmd

import (
	"context"

	"github.com/cedana/cedana/internal/config"
	"github.com/cedana/cedana/internal/server"
	"github.com/cedana/cedana/pkg/types"
	"github.com/rs/zerolog/log"
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
	ctx = log.With().Str("context", "cmd").Logger().WithContext(ctx)

	rootCmd.Version = version
	rootCmd.Long = rootCmd.Long + "\n " + version
	rootCmd.SilenceUsage = true // only show usage when true usage error

	rootCmd.PersistentFlags().String(configFlag, "", "one-time config JSON string (will merge with existing config)")
	rootCmd.PersistentFlags().String(configDirFlag, "", "custom config directory")
	rootCmd.PersistentFlags().Uint32(portFlag, server.DEFAULT_PORT, "port to listen on/connect to")
	rootCmd.PersistentFlags().String(hostFlag, server.DEFAULT_HOST, "host to listen on/connect to")

	// initialize config
	rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		conf, _ := cmd.Flags().GetString(configFlag)
		confDir, _ := cmd.Flags().GetString(configDirFlag)
		if err := config.InitConfig(types.InitConfigArgs{
			Config:    conf,
			ConfigDir: confDir,
		}); err != nil {
			log.Error().Err(err).Msg("failed to initialize config")
			return err
		}
		return nil
	}

	return rootCmd.ExecuteContext(ctx)
}

package cmd

import (
	"context"
	"fmt"

	"github.com/cedana/cedana/internal/config"
	"github.com/cedana/cedana/pkg/flags"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func init() {
	// Add main subcommands
	rootCmd.AddCommand(daemonCmd)
	rootCmd.AddCommand(dumpCmd)
	rootCmd.AddCommand(restoreCmd)
	rootCmd.AddCommand(manageCmd)
	rootCmd.AddCommand(pluginCmd)
	rootCmd.AddCommand(startCmd)
	rootCmd.AddCommand(attachCmd)
	rootCmd.AddCommand(jobCmd)

	// Add aliases

	// Add root flags
	rootCmd.PersistentFlags().
		String(flags.ConfigFlag.Full, "", "one-time config JSON string (will merge with existing config)")
	rootCmd.PersistentFlags().String(flags.ConfigDirFlag.Full, "", "custom config directory")
	rootCmd.MarkPersistentFlagDirname(flags.ConfigDirFlag.Full)
	rootCmd.MarkFlagsMutuallyExclusive(flags.ConfigFlag.Full, flags.ConfigDirFlag.Full)
	rootCmd.PersistentFlags().
		Uint32P(flags.PortFlag.Full, flags.PortFlag.Short, 0, "port to listen on/connect to")
	rootCmd.PersistentFlags().
		StringP(flags.HostFlag.Full, flags.HostFlag.Short, "", "host to listen on/connect to")
	rootCmd.PersistentFlags().
		BoolP(flags.UseVSOCKFlag.Full, flags.UseVSOCKFlag.Short, false, "use vsock for communication")

	// Bind to config
	viper.BindPFlag(config.PORT.Key, rootCmd.PersistentFlags().Lookup(flags.PortFlag.Full))
	viper.BindPFlag(config.PORT.Key, rootCmd.PersistentFlags().Lookup(flags.HostFlag.Full))
	viper.BindPFlag(config.USE_VSOCK.Key, rootCmd.PersistentFlags().Lookup(flags.UseVSOCKFlag.Full))
}

var rootCmd = &cobra.Command{
	Use:   "cedana",
	Short: "Dump/restore container/process",
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

	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		conf, _ := cmd.Flags().GetString(flags.ConfigFlag.Full)
		confDir, _ := cmd.Flags().GetString(flags.ConfigDirFlag.Full)
		if err := config.Init(config.InitArgs{
			Config:    conf,
			ConfigDir: confDir,
		}); err != nil {
			return fmt.Errorf("Failed to initialize config: %w", err)
		}
		return nil
	},
}

func Execute(ctx context.Context, version string) error {
	ctx = log.With().Str("context", "cmd").Logger().WithContext(ctx)

	rootCmd.Version = version
	rootCmd.Long = rootCmd.Long + "\n " + version
	rootCmd.SilenceUsage = true // only show usage when true usage error

	return rootCmd.ExecuteContext(ctx)
}

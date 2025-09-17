package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/cedana/cedana/pkg/config"
	"github.com/cedana/cedana/pkg/features"
	"github.com/cedana/cedana/pkg/flags"
	"github.com/cedana/cedana/pkg/logging"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func init() {
	cobra.EnableTraverseRunHooks = true

	// Add main subcommands
	rootCmd.AddCommand(daemonCmd)
	rootCmd.AddCommand(dumpCmd)
	rootCmd.AddCommand(restoreCmd)
	rootCmd.AddCommand(manageCmd)
	rootCmd.AddCommand(pluginCmd)
	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(attachCmd)
	rootCmd.AddCommand(jobCmd)
	rootCmd.AddCommand(queryCmd)
	rootCmd.AddCommand(docGenCmd)
	rootCmd.AddCommand(dumpVMCmd)
	rootCmd.AddCommand(checkCmd)
	rootCmd.AddCommand(freezeCmd)
	rootCmd.AddCommand(unfreezeCmd)

	// Add helper cmds from plugins
	features.HelperCmds.IfAvailable(
		func(name string, pluginCmds []*cobra.Command) error {
			rootCmd.AddCommand(pluginCmds...)
			return nil
		},
	)

	// Add root flags
	rootCmd.PersistentFlags().
		String(flags.ConfigFlag.Full, "", "one-time config JSON string (merge with existing config)")
	rootCmd.PersistentFlags().String(flags.ConfigDirFlag.Full, "", "custom config directory")
	rootCmd.MarkPersistentFlagDirname(flags.ConfigDirFlag.Full)
	rootCmd.MarkFlagsMutuallyExclusive(flags.ConfigFlag.Full, flags.ConfigDirFlag.Full)
	rootCmd.PersistentFlags().
		StringP(flags.ProtocolFlag.Full, flags.ProtocolFlag.Short, "", "protocol to use (TCP, UNIX, VSOCK)")
	rootCmd.PersistentFlags().
		StringP(flags.AddressFlag.Full, flags.AddressFlag.Short, "", "address to use (host:port for TCP, path for UNIX, cid:port for VSOCK)")
	rootCmd.PersistentFlags().
		BoolP(flags.ProfilingFlag.Full, flags.ProfilingFlag.Short, false, "enable profiling/show profiling data")

	// Bind to config
	viper.BindPFlag("protocol", rootCmd.PersistentFlags().Lookup(flags.ProtocolFlag.Full))
	viper.BindPFlag("address", rootCmd.PersistentFlags().Lookup(flags.AddressFlag.Full))
	viper.BindPFlag("profiling.enabled", rootCmd.PersistentFlags().Lookup(flags.ProfilingFlag.Full))
}

var rootCmd = &cobra.Command{
	Use:   "cedana",
	Short: "Root command for Cedana",
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

		if confDir == "" {
			confDir = os.Getenv("CEDANA_CONFIG_DIR")
		}

		if err := config.Init(config.InitArgs{
			Config:    conf,
			ConfigDir: confDir,
		}); err != nil {
			return fmt.Errorf("Failed to initialize config: %w", err)
		}

		logging.InitLogger(config.Global.LogLevel)

		return nil
	},
}

func Execute(ctx context.Context, version string) error {
	ctx = log.With().Str("context", "cmd").Logger().WithContext(ctx)

	rootCmd.Version = version
	revision := getRevision()
	versionTemplate := rootCmd.VersionTemplate()
	if revision != "" {
		versionTemplate = fmt.Sprintf("git: %s\n%s", revision, versionTemplate)
	}
	rootCmd.SetVersionTemplate(versionTemplate)

	rootCmd.Long = rootCmd.Long + "\n " + version
	rootCmd.SilenceUsage = true // only show usage when true usage error

	return rootCmd.ExecuteContext(ctx)
}

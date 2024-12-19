package cmd

import (
	"fmt"

	"github.com/cedana/cedana/internal/server"
	"github.com/cedana/cedana/pkg/config"
	"github.com/cedana/cedana/pkg/flags"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func init() {
	daemonCmd.AddCommand(startDaemonCmd)

	// Add flags
	startDaemonCmd.PersistentFlags().
		BoolP(flags.MetricsASRFlag.Full, flags.MetricsASRFlag.Short, false, "enable metrics for ASR")
	startDaemonCmd.PersistentFlags().
		StringP(flags.LocalDBFlag.Full, flags.LocalDBFlag.Short, "", "path to local database")

	// Bind to config
	viper.BindPFlag("metrics.asr", startDaemonCmd.PersistentFlags().Lookup(flags.MetricsASRFlag.Full))
}

var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Manage the daemon",
}

var startDaemonCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the daemon",
	RunE: func(cmd *cobra.Command, args []string) error {
		if utils.IsRootUser() == false {
			return fmt.Errorf("daemon must be run as root")
		}

		localDBPath, _ := cmd.Flags().GetString(flags.LocalDBFlag.Full)

		var err error

		log.Info().Str("version", rootCmd.Version).Msg("starting daemon")

		server, err := server.NewServer(cmd.Context(), &server.ServeOpts{
			UseVSOCK:    config.Global.UseVSOCK,
			Port:        config.Global.Port,
			Host:        config.Global.Host,
			Metrics:     config.Global.Metrics,
			LocalDBPath: localDBPath,
			Version:     cmd.Version,
		})
		if err != nil {
			log.Error().Err(err).Msgf("stopping daemon")
			return fmt.Errorf("failed to create server: %w", err)
		}

		err = server.Launch(cmd.Context())
		if err != nil {
			log.Error().Err(err).Msgf("stopping daemon")
			return err
		}

		return nil
	},
}

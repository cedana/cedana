package cmd

import (
	"fmt"

	"github.com/cedana/cedana/internal/config"
	"github.com/cedana/cedana/internal/server"
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

	// Bind to config
	viper.BindPFlag(
		config.METRICS_ASR.Key,
		startDaemonCmd.PersistentFlags().Lookup(flags.MetricsASRFlag.Full),
	)
}

var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Manage the daemon",
}

var startDaemonCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the daemon",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		log := log.Ctx(ctx)
		if utils.IsRootUser() == false {
			return fmt.Errorf("daemon must be run as root")
		}

		var err error

		log.Info().Str("version", rootCmd.Version).Msg("starting daemon")

		server, err := server.NewServer(ctx, &server.ServeOpts{
			UseVSOCK: config.Get(config.USE_VSOCK),
			Port:     config.Get(config.PORT),
			Host:     config.Get(config.HOST),
			Metrics: server.MetricOpts{
				ASR:  config.Get(config.METRICS_ASR),
				OTel: config.Get(config.METRICS_OTEL_ENABLED),
			},
		})
		if err != nil {
			log.Error().Err(err).Msgf("stopping daemon")
			return fmt.Errorf("failed to create server: %w", err)
		}

		err = server.Launch(ctx)
		if err != nil {
			log.Error().Err(err).Msgf("stopping daemon")
			return err
		}

		return nil
	},
}

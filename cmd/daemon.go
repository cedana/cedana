package cmd

import (
	"fmt"
	"os"

	"github.com/cedana/cedana/internal/server"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

var startDaemonCmd = &cobra.Command{
	Use:   "start",
	Short: "Starts the daemon",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		if os.Getuid() != 0 {
			return fmt.Errorf("daemon must be run as root")
		}

		var err error

		vsockEnabled, _ := cmd.Flags().GetBool(vsockEnabledFlag)
		port, _ := cmd.Flags().GetUint32(portFlag)
		metricsEnabled, _ := cmd.Flags().GetBool(metricsEnabledFlag)
		jobServiceEnabled, _ := cmd.Flags().GetBool(jobServiceFlag)

		log.Ctx(ctx).Info().Str("version", rootCmd.Version).Msg("starting daemon")
	  ctx = log.With().Str("context", "daemon").Logger().WithContext(ctx)

		err = api.StartServer(ctx, &server.ServeOpts{
			VSOCKEnabled:      vsockEnabled,
			Port:              port,
		})
		if err != nil {
			log.Ctx(ctx).Error().Err(err).Msgf("stopping daemon")
			return err
		}

		return nil
	},
}

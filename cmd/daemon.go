package cmd

import (
	"fmt"

	"github.com/cedana/cedana/internal/config"
	"github.com/cedana/cedana/internal/server"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

func init() {
	daemonCmd.AddCommand(startDaemonCmd)
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

		useVSOCK := config.Get(config.USE_VSOCK)
		port := config.Get(config.PORT)
		host := config.Get(config.HOST)

		log.Info().Str("version", rootCmd.Version).Msg("starting daemon")

		server, err := server.NewServer(ctx, &server.ServeOpts{
			UseVSOCK: useVSOCK,
			Port:     port,
			Host:     host,
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

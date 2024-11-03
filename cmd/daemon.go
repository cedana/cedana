package cmd

import (
	"fmt"

	"github.com/cedana/cedana/internal/server"
	"github.com/cedana/cedana/internal/utils"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
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

		useVSOCK := viper.GetBool("options.use_vsock")
		port := viper.GetUint32("options.port")
		host := viper.GetString("options.host")

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

		err = server.Launch()
		if err != nil {
			log.Error().Err(err).Msgf("stopping daemon")
			return err
		}

		return nil
	},
}

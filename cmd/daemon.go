package cmd

import (
	"fmt"
	"os"

	"github.com/cedana/cedana/internal/server"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Manage the cedana daemon",
}

var startDaemonCmd = &cobra.Command{
	Use:   "start",
	Short: "Starts the daemon",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		log := log.Ctx(ctx)
		if os.Getuid() != 0 {
			return fmt.Errorf("daemon must be run as root")
		}

		var err error

		vsockEnabled, _ := cmd.Flags().GetBool(vsockEnabledFlag)
		port, _ := cmd.Flags().GetUint32(portFlag)
		host, _ := cmd.Flags().GetString(hostFlag)

		log.Info().Str("version", rootCmd.Version).Msg("starting daemon")

		server, err := server.NewServer(ctx, &server.ServeOpts{
			VSOCKEnabled: vsockEnabled,
			Port:         port,
			Host:         host,
		})
		if err != nil {
			log.Error().Err(err).Msgf("stopping daemon")
			return fmt.Errorf("failed to create server: %w", err)
		}

		err = server.Start()
		if err != nil {
			log.Error().Err(err).Msgf("stopping daemon")
			return err
		}

		return nil
	},
}

func init() {
	daemonCmd.AddCommand(startDaemonCmd)
}

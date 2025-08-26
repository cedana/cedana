package cmd

import (
	"context"
	"fmt"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
	"github.com/cedana/cedana/internal/cedana"
	"github.com/cedana/cedana/pkg/client"
	"github.com/cedana/cedana/pkg/config"
	"github.com/cedana/cedana/pkg/flags"
	"github.com/cedana/cedana/pkg/utils"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func init() {
	daemonCmd.AddCommand(startDaemonCmd)
	daemonCmd.AddCommand(checkDaemonCmd)

	// Add flags
	startDaemonCmd.PersistentFlags().
		StringP(flags.DBFlag.Full, flags.DBFlag.Short, "", "path to local database")
	checkDaemonCmd.PersistentFlags().
		BoolP(flags.FullFlag.Full, flags.FullFlag.Short, false, "perform a full check (including plugins)")

	// Bind to config
	viper.BindPFlag("db.path", startDaemonCmd.PersistentFlags().Lookup(flags.DBFlag.Full))
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

		ctx, cancel := context.WithCancel(cmd.Context())
		defer cancel()

		log.Info().Str("version", rootCmd.Version).Msg("starting daemon")

		server, err := cedana.NewServer(ctx, &cedana.ServeOpts{
			Address:  config.Global.Address,
			Protocol: config.Global.Protocol,
			Version:  cmd.Version,
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

var checkDaemonCmd = &cobra.Command{
	Use:   "check",
	Short: "Health check the daemon",
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := client.New(config.Global.Address, config.Global.Protocol)
		if err != nil {
			return fmt.Errorf("Error creating client: %v", err)
		}

		full, _ := cmd.Flags().GetBool(flags.FullFlag.Full)

		resp, err := client.HealthCheck(cmd.Context(), &daemon.HealthCheckReq{Full: full})
		if err != nil {
			return err
		}

		return printHealthCheckResults(resp.GetResults())
	},
}

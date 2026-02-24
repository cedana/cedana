package cmd

import (
	"context"
	_ "embed"
	"sync"

	"github.com/cedana/cedana/pkg/config"
	"github.com/cedana/cedana/pkg/metrics"
	"github.com/cedana/cedana/pkg/version"
	"github.com/spf13/cobra"
)

//go:embed scripts/setup.sh
var setupScript string

//go:embed scripts/cleanup.sh
var cleanupScript string

func init() {
	HelperCmd.AddCommand(setupCmd)
	HelperCmd.AddCommand(destroyCmd)
}

var HelperCmd = &cobra.Command{
	Use:   "slurm",
	Short: "Helper for setting up and running in Slurm",
	Args:  cobra.ExactArgs(1),
}

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Setup and start cedana on host",
	RunE: func(cmd *cobra.Command, args []string) (err error) {
		ctx, cancel := context.WithCancel(cmd.Context())
		wg := &sync.WaitGroup{}

		defer func() {
			cancel()
			wg.Wait()
		}()

		if config.Global.Metrics {
			metrics.Init(ctx, wg, "cedana-slurm", version.Version)
		}

// 		err = setupDaemon(
// 			ctx,
// 			logging.Writer(
// 				log.With().Str("operation", "setup").Logger().WithContext(ctx),
// 				zerolog.DebugLevel,
// 			),
// 		)
// 		if err != nil {
// 			log.Error().Err(err).Msg("failed to setup daemon")
// 			return fmt.Errorf("error setting up host: %w", err)
// 		}

		return nil
	},
}

var destroyCmd = &cobra.Command{
	Use:   "destroy",
	Short: "Destroy and cleanup cedana on host",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := context.WithCancel(cmd.Context())
		wg := &sync.WaitGroup{}

		defer func() {
			cancel()
			wg.Wait()
		}()

		if config.Global.Metrics {
			metrics.Init(ctx, wg, "cedana-helper", version.Version)
		}

// 		err := destroyDaemon(
// 			ctx,
// 			logging.Writer(
// 				log.With().Str("operation", "destroy").Logger().WithContext(ctx),
// 				zerolog.DebugLevel,
// 			),
// 		)
// 		if err != nil {
// 			log.Error().Err(err).Msg("failed to destroy daemon")
// 			return fmt.Errorf("error destroying host: %w", err)
// 		}

		return nil
	},
}

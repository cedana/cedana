package cmd

import (
	"context"
	_ "embed"
	"fmt"
	"sync"

	"github.com/cedana/cedana/pkg/config"
	"github.com/cedana/cedana/pkg/metrics"
	"github.com/cedana/cedana/pkg/script"
	"github.com/cedana/cedana/pkg/version"
	slurmscripts "github.com/cedana/cedana/plugins/slurm/scripts"
	"github.com/cedana/cedana/scripts"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

func init() {
	HelperCmd.AddCommand(setupCmd)
	HelperCmd.AddCommand(destroyCmd)

	script.Source(scripts.Utils)
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

		err = script.Run(
			log.With().Str("operation", "setup").Logger().Level(zerolog.DebugLevel).WithContext(ctx),
			scripts.ResetService,
			scripts.InstallDeps,
			slurmscripts.Install,
			slurmscripts.InstallPlugins,
			scripts.ConfigureShm,
			scripts.ConfigureIoUring,
			scripts.InstallService,
		)
		if err != nil {
			log.Error().Err(err).Msg("failed to setup daemon")
			return fmt.Errorf("error setting up host: %w", err)
		}

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

		err := script.Run(
			log.With().Str("operation", "destroy").Logger().Level(zerolog.DebugLevel).WithContext(ctx),
			script.Chroot("/host", scripts.ResetService),
			slurmscripts.Uninstall,
		)
		if err != nil {
			log.Error().Err(err).Msg("failed to uninstall cedana")
			return fmt.Errorf("error uninstalling: %w", err)
		}

		return nil
	},
}

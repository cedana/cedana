package cmd

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"strings"
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

const (
	slurmNodeRoleEnv        = "CEDANA_SLURM_NODE_ROLE"
	slurmNodeRoleController = "controller"
	slurmNodeRoleWorker     = "worker"
	slurmNodeRoleLogin      = "login"
)

var (
	setupNodeRole   string
	destroyNodeRole string
)

func init() {
	setupCmd.Flags().StringVar(&setupNodeRole, "node-role", "",
		"SLURM node role: controller, worker or login")
	destroyCmd.Flags().StringVar(&destroyNodeRole, "node-role", "",
		"SLURM node role: controller, worker or login")

	HelperCmd.AddCommand(setupCmd)
	HelperCmd.AddCommand(destroyCmd)

	script.Source(scripts.Utils)
}

// resolveSlurmNodeRole picks the role from the flag, falling back to the env
func resolveSlurmNodeRole(flagValue string) (string, error) {
	role := strings.TrimSpace(flagValue)
	if role == "" {
		role = os.Getenv(slurmNodeRoleEnv)
	}
	if role == "" {
		return "", fmt.Errorf(
			"SLURM node role is required: pass --node-role %q, %q or %q, or set %s",
			slurmNodeRoleController, slurmNodeRoleWorker, slurmNodeRoleLogin, slurmNodeRoleEnv,
		)
	}
	switch strings.ToLower(role) {
	case slurmNodeRoleController:
		return slurmNodeRoleController, nil
	case slurmNodeRoleWorker, "compute":
		return slurmNodeRoleWorker, nil
	case slurmNodeRoleLogin:
		return slurmNodeRoleLogin, nil
	default:
		return "", fmt.Errorf(
			"invalid --node-role %q: must be one of %q, %q or %q",
			role, slurmNodeRoleController, slurmNodeRoleWorker, slurmNodeRoleLogin,
		)
	}
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

		nodeRole, err := resolveSlurmNodeRole(setupNodeRole)
		if err != nil {
			return err
		}
		if err := os.Setenv(slurmNodeRoleEnv, nodeRole); err != nil {
			return fmt.Errorf("failed to set %s: %w", slurmNodeRoleEnv, err)
		}

		if nodeRole == slurmNodeRoleLogin {
			log.Info().Msg("login node: nothing to set up")
			return nil
		}

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

		nodeRole, err := resolveSlurmNodeRole(destroyNodeRole)
		if err != nil {
			return err
		}
		if err := os.Setenv(slurmNodeRoleEnv, nodeRole); err != nil {
			return fmt.Errorf("failed to set %s: %w", slurmNodeRoleEnv, err)
		}

		if nodeRole == slurmNodeRoleLogin {
			log.Info().Msg("login node: nothing to destroy")
			return nil
		}

		if config.Global.Metrics {
			metrics.Init(ctx, wg, "cedana-helper", version.Version)
		}

		err = script.Run(
			log.With().Str("operation", "destroy").Logger().Level(zerolog.DebugLevel).WithContext(ctx),
			scripts.ResetService,
			slurmscripts.Uninstall,
		)
		if err != nil {
			log.Error().Err(err).Msg("failed to uninstall cedana")
			return fmt.Errorf("error uninstalling: %w", err)
		}

		return nil
	},
}
